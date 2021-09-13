package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/match"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/uuid"

	"github.com/rancherfederal/hauler/pkg/log"

	"github.com/opencontainers/go-digest"
	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type OciLayout struct {
	layout layout.Path

	// root is the root directory for the OciLayout
	root string

	lock *sync.RWMutex

	blobCache map[string][]byte

	log log.Logger

	cacheDir string
}

// NewOciLayout returns a Oci store at root, will create one if doesn't exist
func NewOciLayout(root string) (*OciLayout, error) {
	layoutPath, err := layout.FromPath(root)
	if os.IsNotExist(err) {
		if layoutPath, err = layout.Write(root, empty.Index); err != nil {
			return nil, err
		}
	}

	tmpdir, err := os.MkdirTemp("", "hauler-oci-layout")
	if err != nil {
		return nil, err
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	return &OciLayout{
		layout: layoutPath,
		root: absRoot,
		blobCache: make(map[string][]byte),
		cacheDir: tmpdir,
	}, nil
}

// Add will add a remote image to an OCI store only if it doesn't already exist
func (o OciLayout) Add(ref name.Reference, opts ...remote.Option) error {
	image, err := remote.Image(ref, opts...)
	if err != nil {
		if te, ok := err.(*transport.Error); ok {
			if te.StatusCode != http.StatusNotFound {
				return te
			}
		}
	}

	annotations := indexAnnotations(ref)

	// TODO: This won't work for multi-arch images with the current annotations list
	return o.layout.ReplaceImage(image, match.Annotation(ocispecv1.AnnotationRefName, ref.Name()), layout.WithAnnotations(annotations))
}

func indexAnnotations(ref name.Reference) map[string]string {
	annotations := make(map[string]string)
	annotations[ocispecv1.AnnotationRefName] = ref.Identifier()
	annotations[AnnotationRepository] = ref.Context().Name()
	annotations[ocispecv1.AnnotationVendor] = haulerAnnotationVendorName

	return annotations
}

// // TODO: Change this name
// func (o OciLayout) AddGeneric(mediaType string, filename ...string) error {
// 	ctx := context.Background()

// 	var files []v1.Descriptor

// 	fmt.Println("new store at: ", o.root)
// 	store := content.NewFileStore(o.root)

// 	config, err := store.Add("$config", "something.rancher.something.v1+json", "/dev/null")
// 	if err != nil {
// 		return err
// 	}

// 	if config.Annotations == nil {
// 		config.Annotations = make(map[string]string)
// 	}

// 	ref, err := name.ParseReference("hauler/something:v1")
// 	for k, v := range indexAnnotations(ref) {
// 		config.Annotations[k] = v
// 	}

// 	wr, err := store.Writer(ctx)
// 	defer wr.Close()
// 	if err != nil {
// 		return err
// 	}

// 	for _, fn := range filename {
// 		name := filepath.Clean(fn)
// 		if !filepath.IsAbs(name) {
// 			name = filepath.ToSlash(name)
// 		}

// 		desc, err := store.Add(name, mediaType, fn)
// 		if err != nil {
// 			return err
// 		}

// 		fmt.Println(desc.Digest.String())
// 		if err := wr.Commit(ctx, desc.Size, desc.Digest); err != nil {
// 			return err
// 		}

// 		ra, err := storeReaderAt(ctx, desc)
// 		if err != nil {
// 			return err
// 		}
// 		defer ra.Close()

// 		rd := io.NewSectionReader(ra, 0, desc.Size)
// 		if err := o.layout.WriteBlob(ociDescriptorToGCR(desc).Digest, ioutil.NopCloser(rd)); err != nil {
// 			return err
// 		}

// 		files = append(files, ociDescriptorToGCR(desc))
// 	}

// 	// Manifest
// 	manifest := v1.Manifest{
// 		SchemaVersion: 2,
// 		MediaType:     "",

// 		// TODO: Custom config type?
// 		Config:        ociDescriptorToGCR(config),

// 		Layers:        files,
// 		Annotations:   map[string]string{},
// 	}

// 	manifestBytes, err := json.Marshal(manifest)
// 	if err != nil {
// 		return err
// 	}

// 	err = o.layout.AppendDescriptor(ociDescriptorToGCR(config))
// 	if err != nil {
// 		return err
// 	}

// 	return o.layout.WriteBlob(manifest.Config.Digest, ioutil.NopCloser(bytes.NewReader(manifestBytes)))
// }

// // TODO: There are better ways to do this
// func ociDescriptorToGCR(d ocispecv1.Descriptor) v1.Descriptor {
// 	// TODO: I'm lazy
// 	// optional fields are the woooorst
// 	if d.Platform == nil {
// 		d.Platform = &ocispecv1.Platform{}
// 	}

// 	return v1.Descriptor{
// 		MediaType:   types.MediaType(d.MediaType),
// 		Size:        d.Size,
// 		Digest:      v1.Hash{
// 			Algorithm: string(d.Digest.Algorithm()),
// 			Hex:       d.Digest.Hex(),
// 		},
// 		URLs:        d.URLs,
// 		Annotations: d.Annotations,
// 	}
// }

// Remove will remove an image from an OCI store
//  This will preserve any layers used by other images
func (o OciLayout) Remove() error {
	// TODO:
	idx, _ := o.layout.ImageIndex()
	im, _ := idx.IndexManifest()

	_ = im
	return nil
}

func (o OciLayout) Index() (v1.ImageIndex, error) {
	return o.layout.ImageIndex()
}

// Given a hash, return the matching blob from the layout
func (o OciLayout) GetBlob(repo name.Reference, h v1.Hash) (io.ReadCloser, error) {
	return o.layout.Blob(h)
}

func (o OciLayout) GetBlobWritePath(u string) (*os.File, string, error) {
	if u == "" {
		u = uuid.New().String()

		newBlobWritePath := filepath.Join(o.root, u)
		// Ensure file exists
		f, err := os.OpenFile(newBlobWritePath, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			return nil, "", err
		}

		return f, u, nil
	}

	blobWritePath := filepath.Join(o.root, u)

	if _, err := os.Stat(blobWritePath); os.IsNotExist(err) {
		return nil, "", err
	} else if err != nil {
		return nil, "", err
	}

	f, err := os.OpenFile(blobWritePath, os.O_WRONLY, 0666)
	if err != nil {
		return nil, "", err
	}

	return f, u, nil
}

func (o OciLayout) WriteManifest(m *v1.Manifest) error {
	ii, _ := o.layout.ImageIndex()
	im, _ := ii.IndexManifest()

	im.Manifests = append(im.Manifests, m.Config)

	if err := o.layout.WriteIndex(ii); err != nil {
		return err
	}

	return o.layout.AppendDescriptor(m.Config)

	data, err := json.Marshal(m)
	if err != nil {
		return err
	}

	return o.layout.WriteBlob(m.Config.Digest, ioutil.NopCloser(bytes.NewBuffer(data)))
}

func (o OciLayout) GetImageManifest(repo string, ref string) (v1.Descriptor, io.ReadCloser, error) {
	fullRef, err := ParseRepoAndReference(repo, ref)
	if err != nil {
		return v1.Descriptor{}, nil, err
	}

	idx, _ := o.layout.ImageIndex()
	idxManifest, _ := idx.IndexManifest()

	found := false
	var d v1.Descriptor
	for _, descriptor := range idxManifest.Manifests {
		if v, ok := descriptor.Annotations[AnnotationRepository]; ok {
			if v != fullRef.Context().Name() {
				continue
			}

			// Digest <-> Digest
			if descriptor.Digest.String() == fullRef.Identifier() {
				found = true
				d = descriptor
			}

			// Tag <-> Tag
			if vv, ok := descriptor.Annotations[ocispecv1.AnnotationRefName]; ok {
				if vv == fullRef.Identifier() {
					found = true
					d = descriptor
				}
			}
		}
	}

	if !found {
		// TODO: Replace with real error
		return v1.Descriptor{}, nil, fmt.Errorf("not found")
	}

	b, err := o.layout.Blob(d.Digest)
	return d, b, err
}

func (o OciLayout) NewBlobCache() (string, error) {
	u := uuid.New()
	o.blobCache[u.String()] = nil

	f, err := os.OpenFile(filepath.Join(o.cacheDir, u.String()), os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0666)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return u.String(), nil
}

func (o OciLayout) getBlobCache(loc string) (string, error) {
	if _, ok := o.blobCache[loc]; !ok {
		return "", fmt.Errorf("blob cache does not exist")
	}

	return filepath.Join(o.cacheDir, loc), nil
}

func (o OciLayout) WriteBlob(h v1.Hash, body io.Reader, loc string) error {
	if loc == "" {
		// Mono blob
		return o.layout.WriteBlob(h, ioutil.NopCloser(body))
	}

	blobPath, err := o.getBlobCache(loc)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(blobPath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer os.Remove(blobPath)

	// Make the final write
	_, err = io.Copy(f, body)
	if err != nil {
		return err
	}
	_ = f.Close()

	// TODO: Validate digest with complete
	return os.Rename(blobPath, filepath.Join(o.root, "blobs", h.Algorithm, h.Hex))
}

func (o OciLayout) PatchBlob(body io.Reader, from, to int64, loc string) (int64, error) {
	blobPath, err := o.getBlobCache(loc)
	if err != nil {
		return -1, err
	}

	fi, err := os.Stat(blobPath)
	if err != nil {
		return -1, err
	}

	if from != fi.Size() {
		return -1, fmt.Errorf("from is not correct")
	}

	f, err := os.OpenFile(blobPath, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return -1, fmt.Errorf("to open blob cache file")
	}

	// Forward to end of file
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return -1, err
	}

	written, err := io.Copy(f, body)
	if err != nil {
		return -1, err
	}

	return written, nil
}

func (o OciLayout) RLock() {
	o.lock.RLock()
}

func (o OciLayout) RUnlock() {
	o.lock.RUnlock()
}

func (o OciLayout) WLock() {
	o.lock.Lock()
}

func (o OciLayout) WUnlock() {
	o.lock.Unlock()
}

// digestMatches compares a given digest with the digest computed from a reader
func DigestMatches(have string, data []byte) bool {
	want := digest.FromBytes(data)

	dhave, err := digest.Parse(have)
	if err != nil {
		return false
	}

	return want.String() == dhave.String()
}
