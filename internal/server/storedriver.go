package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"slices"
	"time"

	storagedriver "github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/distribution/distribution/v3/registry/storage/driver/factory"
	"github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	goname "github.com/google/go-containerregistry/pkg/name"
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/reference"
)

// StoreDriverName is the distribution storage driver that serves a hauler store in place.
// Parameters: "store" (the hauler store dir) and "rootdirectory" (a filesystem driver dir
// that takes any writes, e.g. pushes when the registry isn't readonly).
const StoreDriverName = "hauler"

// v2Root is the fixed prefix distribution puts on every storage path.
const v2Root = "/docker/registry/v2"

func init() {
	factory.Register(StoreDriverName, storeDriverFactory{})
}

type storeDriverFactory struct{}

func (storeDriverFactory) Create(ctx context.Context, params map[string]any) (storagedriver.StorageDriver, error) {
	storeDir, _ := params["store"].(string)
	if storeDir == "" {
		return nil, fmt.Errorf("%s storage driver: missing [store] parameter", StoreDriverName)
	}
	base, err := filesystem.FromParameters(params)
	if err != nil {
		return nil, err
	}
	return newStoreDriver(storeDir, base)
}

// vfile is one synthesized file: either a small in-memory link or a blob in the store.
type vfile struct {
	data []byte
	blob string
}

// storeDriver serves the registry storage layout (blob data, repository layer/manifest/tag
// links) straight out of a hauler OCI layout, so `store serve registry` needs no copy of the
// store. Paths it doesn't know fall through to base. The tree is built once at startup;
// changes to the store need a restart.
type storeDriver struct {
	base  storagedriver.StorageDriver
	files map[string]vfile
	dirs  map[string][]string // dir -> sorted child paths
	built time.Time
}

func newStoreDriver(storeDir string, base storagedriver.StorageDriver) (*storeDriver, error) {
	o, err := content.NewOCI(storeDir)
	if err != nil {
		return nil, err
	}
	subjects, err := content.SubjectDigests(o)
	if err != nil {
		return nil, err
	}

	d := &storeDriver{base: base, files: map[string]vfile{}, built: time.Now()}
	blobPath := func(dg digest.Digest) string {
		return o.ResolvePath(path.Join(ocispec.ImageBlobsDir, dg.Algorithm().String(), dg.Encoded()))
	}
	link := func(p string, dg digest.Digest) { d.files[p] = vfile{data: []byte(dg.String())} }

	exclude := regexp.MustCompile(consts.FileExcludePattern)
	err = o.Walk(func(_ string, desc ocispec.Descriptor) error {
		baseRef := desc.Annotations[ocispec.AnnotationRefName]
		if baseRef == "" || exclude.MatchString(baseRef) {
			return nil
		}
		ref, err := reference.ParseReference(content.RegistryDestRef(desc, subjects))
		if err != nil {
			return nil // copy skips unparseable refs too
		}
		repo := path.Join(v2Root, "repositories", ref.Context().RepositoryStr())

		// link every manifest and blob reachable from desc into repo, as a push would
		var visit func(ocispec.Descriptor) error
		visit = func(desc ocispec.Descriptor) error {
			bp := blobPath(desc.Digest)
			if _, err := os.Stat(bp); err != nil {
				return nil // not in the store (e.g. platforms filtered out at add time)
			}
			d.files[path.Join(v2Root, "blobs", desc.Digest.Algorithm().String(), desc.Digest.Encoded()[:2], desc.Digest.Encoded(), "data")] = vfile{blob: bp}
			if !isManifest(desc.MediaType) {
				link(path.Join(repo, "_layers", desc.Digest.Algorithm().String(), desc.Digest.Encoded(), "link"), desc.Digest)
				return nil
			}
			link(path.Join(repo, "_manifests", "revisions", desc.Digest.Algorithm().String(), desc.Digest.Encoded(), "link"), desc.Digest)

			raw, err := os.ReadFile(bp)
			if err != nil {
				return err
			}
			var m struct {
				Config    *ocispec.Descriptor  `json:"config"`
				Layers    []ocispec.Descriptor `json:"layers"`
				Manifests []ocispec.Descriptor `json:"manifests"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				return fmt.Errorf("decoding manifest %s: %w", desc.Digest, err)
			}
			children := append(m.Layers, m.Manifests...)
			if m.Config != nil {
				children = append(children, *m.Config)
			}
			for _, c := range children {
				if err := visit(c); err != nil {
					return err
				}
			}
			return nil
		}
		if err := visit(desc); err != nil {
			return err
		}

		if tag, ok := ref.(goname.Tag); ok {
			tagDir := path.Join(repo, "_manifests", "tags", tag.TagStr())
			link(path.Join(tagDir, "current", "link"), desc.Digest)
			link(path.Join(tagDir, "index", desc.Digest.Algorithm().String(), desc.Digest.Encoded(), "link"), desc.Digest)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	d.dirs = map[string][]string{}
	for p := range d.files {
		for child := p; child != "/" && !seen[child]; child = path.Dir(child) {
			seen[child] = true
			d.dirs[path.Dir(child)] = append(d.dirs[path.Dir(child)], child)
		}
	}
	for _, c := range d.dirs {
		slices.Sort(c)
	}
	return d, nil
}

func isManifest(mt string) bool {
	switch mt {
	case ocispec.MediaTypeImageManifest, ocispec.MediaTypeImageIndex, consts.DockerManifestSchema2, consts.DockerManifestListSchema2:
		return true
	}
	return false
}

func (d *storeDriver) Name() string { return StoreDriverName }

func (d *storeDriver) GetContent(ctx context.Context, p string) ([]byte, error) {
	f, ok := d.files[p]
	switch {
	case !ok:
		return d.base.GetContent(ctx, p)
	case f.blob != "":
		return os.ReadFile(f.blob)
	default:
		return f.data, nil
	}
}

func (d *storeDriver) Reader(ctx context.Context, p string, offset int64) (io.ReadCloser, error) {
	f, ok := d.files[p]
	switch {
	case !ok:
		return d.base.Reader(ctx, p, offset)
	case f.blob != "":
		fh, err := os.Open(f.blob)
		if err != nil {
			return nil, err
		}
		if _, err := fh.Seek(offset, io.SeekStart); err != nil {
			fh.Close()
			return nil, err
		}
		return fh, nil
	default:
		if offset > int64(len(f.data)) {
			return nil, storagedriver.InvalidOffsetError{Path: p, Offset: offset}
		}
		return io.NopCloser(bytes.NewReader(f.data[offset:])), nil
	}
}

func (d *storeDriver) Stat(ctx context.Context, p string) (storagedriver.FileInfo, error) {
	fields := storagedriver.FileInfoFields{Path: p, ModTime: d.built}
	if f, ok := d.files[p]; ok {
		fields.Size = int64(len(f.data))
		if f.blob != "" {
			fi, err := os.Stat(f.blob)
			if err != nil {
				return nil, err
			}
			fields.Size, fields.ModTime = fi.Size(), fi.ModTime()
		}
		return storagedriver.FileInfoInternal{FileInfoFields: fields}, nil
	}
	if _, ok := d.dirs[p]; ok {
		fields.IsDir = true
		return storagedriver.FileInfoInternal{FileInfoFields: fields}, nil
	}
	return d.base.Stat(ctx, p)
}

func (d *storeDriver) List(ctx context.Context, p string) ([]string, error) {
	children, ok := d.dirs[p]
	fromBase, err := d.base.List(ctx, p)
	if err != nil {
		if !ok || !errors.As(err, new(storagedriver.PathNotFoundError)) {
			return nil, err
		}
	}
	merged := slices.Concat(children, fromBase)
	slices.Sort(merged)
	return slices.Compact(merged), nil
}

// readOnly rejects changes to paths served from the store, which would otherwise be silently
// shadowed by (or delete) store content.
func (d *storeDriver) readOnly(p string) error {
	if _, ok := d.files[p]; ok {
		return fmt.Errorf("%s: served from the hauler store and read-only", p)
	}
	if _, ok := d.dirs[p]; ok {
		return fmt.Errorf("%s: served from the hauler store and read-only", p)
	}
	return nil
}

func (d *storeDriver) PutContent(ctx context.Context, p string, data []byte) error {
	if f, ok := d.files[p]; ok && f.blob == "" && bytes.Equal(f.data, data) {
		return nil // re-pushing what the store already serves
	}
	if err := d.readOnly(p); err != nil {
		return err
	}
	return d.base.PutContent(ctx, p, data)
}

func (d *storeDriver) Writer(ctx context.Context, p string, append bool) (storagedriver.FileWriter, error) {
	if err := d.readOnly(p); err != nil {
		return nil, err
	}
	return d.base.Writer(ctx, p, append)
}

func (d *storeDriver) Move(ctx context.Context, src, dst string) error {
	if _, ok := d.files[dst]; ok {
		// an upload of a blob the store already has: drop the duplicate, the store copy serves
		return d.base.Delete(ctx, src)
	}
	if err := d.readOnly(src); err != nil {
		return err
	}
	return d.base.Move(ctx, src, dst)
}

func (d *storeDriver) Delete(ctx context.Context, p string) error {
	if err := d.readOnly(p); err != nil {
		return err
	}
	return d.base.Delete(ctx, p)
}

func (d *storeDriver) RedirectURL(*http.Request, string) (string, error) { return "", nil }

func (d *storeDriver) Walk(ctx context.Context, p string, f storagedriver.WalkFn, opts ...func(*storagedriver.WalkOptions)) error {
	return storagedriver.WalkFallback(ctx, d, p, f, opts...)
}
