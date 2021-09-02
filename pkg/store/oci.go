package store

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/match"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type Oci struct {
	Name   string
	layout layout.Path
}

// NewOci returns a Oci store at path, will create one if doesn't exist
func NewOci(path string) (*Oci, error) {
	layoutPath, err := layout.FromPath(path)
	if os.IsNotExist(err) {
		if layoutPath, err = layout.Write(path, empty.Index); err != nil {
			return nil, err
		}
	}

	return &Oci{
		layout: layoutPath,
	}, nil
}

// Add will add a remote image to an OCI store only if it doesn't already exist
func (o Oci) Add(ref name.Reference, opts ...remote.Option) error {
	image, err := remote.Image(ref, opts...)
	if err != nil {
		if te, ok := err.(*transport.Error); ok {
			if te.StatusCode != http.StatusNotFound {
				return te
			}
		}
	}

	// TODO: Factor this out since naming is important (used for layout lookups)
	annotations := make(map[string]string)
	annotations[ocispecv1.AnnotationRefName] = ref.Identifier()
	annotations[AnnotationRepository] = ref.Context().Name()
	annotations[ocispecv1.AnnotationVendor] = haulerAnnotationVendorName

	// TODO: This won't work for multi-arch images with the current annotations list
	return o.layout.ReplaceImage(image, match.Annotation(ocispecv1.AnnotationRefName, ref.Name()), layout.WithAnnotations(annotations))
}

// Remove will remove an image from an OCI store
//  This will preserve any layers used by other images
func (o Oci) Remove() error {
	// TODO:
	idx, _ := o.layout.ImageIndex()
	im, _ := idx.IndexManifest()

	_ = im
	return nil
}

func (o Oci) Index() (v1.ImageIndex, error) {
	return o.layout.ImageIndex()
}

// Given a hash, return the matching blob from the layout
func (o Oci) Blob(repo name.Reference, h v1.Hash) (io.ReadCloser, error) {
	return o.layout.Blob(h)
}

func (o Oci) ImageManifest(repo string, ref string) (v1.Descriptor, io.ReadCloser, error) {
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
		return v1.Descriptor{}, nil, fmt.Errorf("not found")
	}

	b, err := o.layout.Blob(d.Digest)
	return d, b, err
}
