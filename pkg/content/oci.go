package content

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	ccontent "github.com/containerd/containerd/content"
	"github.com/containerd/containerd/remotes"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/target"

	"github.com/hauler-dev/hauler/pkg/consts"
)

var _ target.Target = (*OCI)(nil)

type OCI struct {
	root    string
	index   *ocispec.Index
	nameMap *sync.Map // map[string]ocispec.Descriptor
}

func NewOCI(root string) (*OCI, error) {
	o := &OCI{
		root:    root,
		nameMap: &sync.Map{},
	}
	return o, nil
}

// AddIndex adds a descriptor to the index and updates it
//
//	The descriptor must use AnnotationRefName to identify itself
func (o *OCI) AddIndex(desc ocispec.Descriptor) error {
	if _, ok := desc.Annotations[ocispec.AnnotationRefName]; !ok {
		return fmt.Errorf("descriptor must contain a reference from the annotation: %s", ocispec.AnnotationRefName)
	}
	key := fmt.Sprintf("%s-%s-%s", desc.Digest.String(), desc.Annotations[ocispec.AnnotationRefName], desc.Annotations[consts.KindAnnotationName])
	o.nameMap.Store(key, desc)
	return o.SaveIndex()
}

// LoadIndex will load the index from disk
func (o *OCI) LoadIndex() error {
	path := o.path(consts.OCIImageIndexFile)
	idx, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		o.index = &ocispec.Index{
			Versioned: specs.Versioned{
				SchemaVersion: 2,
			},
		}
		return nil
	}
	defer idx.Close()

	if err := json.NewDecoder(idx).Decode(&o.index); err != nil {
		return err
	}

	for _, desc := range o.index.Manifests {
		key := fmt.Sprintf("%s-%s-%s", desc.Digest.String(), desc.Annotations[ocispec.AnnotationRefName], desc.Annotations[consts.KindAnnotationName])
		if strings.TrimSpace(key) != "--" {
			o.nameMap.Store(key, desc)
		}
	}
	return nil
}

// SaveIndex will update the index on disk
func (o *OCI) SaveIndex() error {
	var descs []ocispec.Descriptor
	o.nameMap.Range(func(name, desc interface{}) bool {
		n := desc.(ocispec.Descriptor).Annotations[ocispec.AnnotationRefName]
		d := desc.(ocispec.Descriptor)

		if d.Annotations == nil {
			d.Annotations = make(map[string]string)
		}
		d.Annotations[ocispec.AnnotationRefName] = n
		descs = append(descs, d)
		return true
	})

	// sort index to ensure that images come before any signatures and attestations.
	sort.SliceStable(descs, func(i, j int) bool {
		kindI := descs[i].Annotations["kind"]
		kindJ := descs[j].Annotations["kind"]

		// Objects with the prefix of "dev.cosignproject.cosign/image" should be at the top.
		if strings.HasPrefix(kindI, consts.KindAnnotation) && !strings.HasPrefix(kindJ, consts.KindAnnotation) {
			return true
		} else if !strings.HasPrefix(kindI, consts.KindAnnotation) && strings.HasPrefix(kindJ, consts.KindAnnotation) {
			return false
		}
		return false // Default: maintain the order.
	})

	o.index.Manifests = descs
	data, err := json.Marshal(o.index)
	if err != nil {
		return err
	}
	return os.WriteFile(o.path(consts.OCIImageIndexFile), data, 0644)
}

// Resolve attempts to resolve the reference into a name and descriptor.
//
// The argument `ref` should be a scheme-less URI representing the remote.
// Structurally, it has a host and path. The "host" can be used to directly
// reference a specific host or be matched against a specific handler.
//
// The returned name should be used to identify the referenced entity.
// Dependending on the remote namespace, this may be immutable or mutable.
// While the name may differ from ref, it should itself be a valid ref.
//
// If the resolution fails, an error will be returned.
func (o *OCI) Resolve(ctx context.Context, ref string) (name string, desc ocispec.Descriptor, err error) {
	if err := o.LoadIndex(); err != nil {
		return "", ocispec.Descriptor{}, err
	}
	d, ok := o.nameMap.Load(ref)
	if !ok {
		return "", ocispec.Descriptor{}, err
	}
	desc = d.(ocispec.Descriptor)
	return ref, desc, nil
}

// Fetcher returns a new fetcher for the provided reference.
// All content fetched from the returned fetcher will be
// from the namespace referred to by ref.
func (o *OCI) Fetcher(ctx context.Context, ref string) (remotes.Fetcher, error) {
	if err := o.LoadIndex(); err != nil {
		return nil, err
	}
	if _, ok := o.nameMap.Load(ref); !ok {
		return nil, nil
	}
	return o, nil
}

func (o *OCI) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	readerAt, err := o.blobReaderAt(desc)
	if err != nil {
		return nil, err
	}
	return readerAt, nil
}

func (o *OCI) FetchManifest(ctx context.Context, manifest ocispec.Manifest) (io.ReadCloser, error) {
	readerAt, err := o.manifestBlobReaderAt(manifest)
	if err != nil {
		return nil, err
	}
	return readerAt, nil
}

// Pusher returns a new pusher for the provided reference
// The returned Pusher should satisfy content.Ingester and concurrent attempts
// to push the same blob using the Ingester API should result in ErrUnavailable.
func (o *OCI) Pusher(ctx context.Context, ref string) (remotes.Pusher, error) {
	if err := o.LoadIndex(); err != nil {
		return nil, err
	}

	var baseRef, hash string
	parts := strings.SplitN(ref, "@", 2)
	baseRef = parts[0]
	if len(parts) > 1 {
		hash = parts[1]
	}
	return &ociPusher{
		oci:    o,
		ref:    baseRef,
		digest: hash,
	}, nil
}

func (o *OCI) Walk(fn func(reference string, desc ocispec.Descriptor) error) error {
	if err := o.LoadIndex(); err != nil {
		return err
	}

	var errst []string
	o.nameMap.Range(func(key, value interface{}) bool {
		if err := fn(key.(string), value.(ocispec.Descriptor)); err != nil {
			errst = append(errst, err.Error())
		}
		return true
	})
	if errst != nil {
		return fmt.Errorf(strings.Join(errst, "; "))
	}
	return nil
}

func (o *OCI) blobReaderAt(desc ocispec.Descriptor) (*os.File, error) {
	blobPath, err := o.ensureBlob(desc.Digest.Algorithm().String(), desc.Digest.Hex())
	if err != nil {
		return nil, err
	}
	return os.Open(blobPath)
}

func (o *OCI) manifestBlobReaderAt(manifest ocispec.Manifest) (*os.File, error) {
	blobPath, err := o.ensureBlob(string(manifest.Config.Digest.Algorithm().String()), manifest.Config.Digest.Hex())
	if err != nil {
		return nil, err
	}
	return os.Open(blobPath)
}

func (o *OCI) blobWriterAt(desc ocispec.Descriptor) (*os.File, error) {
	blobPath, err := o.ensureBlob(desc.Digest.Algorithm().String(), desc.Digest.Hex())
	if err != nil {
		return nil, err
	}
	return os.OpenFile(blobPath, os.O_WRONLY|os.O_CREATE, 0644)
}

func (o *OCI) ensureBlob(alg string, hex string) (string, error) {
	dir := o.path("blobs", alg)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil && !os.IsExist(err) {
		return "", err
	}
	return filepath.Join(dir, hex), nil
}

func (o *OCI) path(elem ...string) string {
	complete := []string{string(o.root)}
	return filepath.Join(append(complete, elem...)...)
}

type ociPusher struct {
	oci    *OCI
	ref    string
	digest string
}

// Push returns a content writer for the given resource identified
// by the descriptor.
func (p *ociPusher) Push(ctx context.Context, d ocispec.Descriptor) (ccontent.Writer, error) {
	switch d.MediaType {
	case ocispec.MediaTypeImageManifest, ocispec.MediaTypeImageIndex, consts.DockerManifestSchema2, consts.DockerManifestListSchema2:
		// if the hash of the content matches that which was provided as the hash for the root, mark it
		if p.digest != "" && p.digest == d.Digest.String() {
			if err := p.oci.LoadIndex(); err != nil {
				return nil, err
			}
			p.oci.nameMap.Store(p.ref, d)
			if err := p.oci.SaveIndex(); err != nil {
				return nil, err
			}
		}
	}

	blobPath, err := p.oci.ensureBlob(d.Digest.Algorithm().String(), d.Digest.Hex())
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(blobPath); err == nil {
		// file already exists, discard (but validate digest)
		return content.NewIoContentWriter(io.Discard, content.WithOutputHash(d.Digest)), nil
	}

	f, err := os.Create(blobPath)
	if err != nil {
		return nil, err
	}

	w := content.NewIoContentWriter(f, content.WithInputHash(d.Digest), content.WithOutputHash(d.Digest))
	return w, nil
}
