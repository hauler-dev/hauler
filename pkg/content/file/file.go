package file

import (
	"io"
	"net/http"
	"os"
	"strings"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/artifact/local"
	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

var _ artifact.OCI = (*file)(nil)

type file struct {
	blob    gv1.Layer
	config  config
	blobMap map[gv1.Hash]gv1.Layer

	annotations map[string]string
}

func NewFile(ref string, filename string) (artifact.OCI, error) {
	var getter local.Opener
	if strings.HasPrefix(ref, "http") || strings.HasPrefix(ref, "https") {
		getter = remoteOpener(ref)
	} else {
		getter = localOpener(ref)
	}

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = filename // For oras FileStore to recognize
	annotations[ocispec.AnnotationSource] = ref

	blob, err := local.LayerFromOpener(getter,
		local.WithMediaType(types.FileLayerMediaType),
		local.WithAnnotations(annotations))
	if err != nil {
		return nil, err
	}

	f := &file{
		blob: blob,
		config: config{
			Reference: ref,
			Name:      filename,
		},
	}
	return f, nil
}

func (f *file) MediaType() string {
	return types.OCIManifestSchema1
}

func (f *file) RawConfig() ([]byte, error) {
	return f.config.Raw()
}

func (f *file) Layers() ([]gv1.Layer, error) {
	var layers []gv1.Layer
	layers = append(layers, f.blob)
	return layers, nil
}

func (f *file) Manifest() (*gv1.Manifest, error) {
	desc, err := partial.Descriptor(f.blob)
	if err != nil {
		return nil, err
	}
	layerDescs := []gv1.Descriptor{*desc}

	cfgDesc, err := f.config.Descriptor()
	if err != nil {
		return nil, err
	}

	return &gv1.Manifest{
		SchemaVersion: 2,
		MediaType:     gtypes.MediaType(f.MediaType()),
		Config:        cfgDesc,
		Layers:        layerDescs,
		Annotations:   f.annotations,
	}, nil
}

func localOpener(path string) local.Opener {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

func remoteOpener(url string) local.Opener {
	return func() (io.ReadCloser, error) {
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
}
