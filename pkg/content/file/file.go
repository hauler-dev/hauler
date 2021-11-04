package file

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	gv1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rancherfederal/hauler/pkg/artifact/v1"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/types"
)

const (
	LayerMediaType = "application/vnd.hauler.cattle.io-artifact"
)

var _ v1.OCICore = (*file)(nil)

// func (f *file) MediaType() types.MediaType {
// 	return f.mediaType
// }
//
// func (f *file) RawManifest() ([]byte, error) {
// 	if err := f.compute(); err != nil {
// 		return nil, err
// 	}
// 	return json.Marshal(f.manifest)
// }
//
// func (f *file) RawConfig() ([]byte, error) {
// 	if err := f.compute(); err != nil {
// 		return nil, err
// 	}
// 	return json.Marshal(f.config)
// }
//
// func (f *file) Layers() ([]gv1.Layer, error) {
// 	if err := f.compute(); err != nil {
// 		return nil, nil
// 	}
// 	return f.layers, nil
// }
//
// func (f *file) compute() error {
// 	if f.computed {
// 		return nil
// 	}
//
// 	c := &fileConfig{Sup: "mom"}
//
// 	digestMap := make(map[gv1.Hash]gv1.Layer)
//
// 	if f.manifest == nil {
// 		ann := make(map[string]string, 0)
// 		ann["donkey"] = "butt"
// 		f.manifest = &gv1.Manifest{
// 			SchemaVersion: 2,
// 			MediaType:     "donkeybutt",
// 			Annotations:   ann,
// 		}
// 	}
//
// 	manifest := f.manifest.DeepCopy()
// 	manifestLayers := manifest.Layers
//
// 	for _, l := range f.layers {
// 		if l == nil {
// 			continue
// 		}
//
// 		desc, err := partial.Descriptor(l)
// 		if err != nil {
// 			return err
// 		}
//
// 		manifestLayers = append(manifestLayers, *desc)
// 		digestMap[desc.Digest] = l
// 	}
//
// 	manifest.Layers = manifestLayers
//
// 	f.manifest = manifest
// 	f.config = c
// 	f.digestMap = digestMap
// 	f.computed = true
// 	return nil
// }

type file struct {
	v1.OCICore

	computed bool
	// manifest  *gv1.Manifest
	// mediaType types.MediaType
	config    *fileConfig
	// layers    []gv1.Layer
	// digestMap map[gv1.Hash]gv1.Layer
}

type fileConfig struct {
	Sup string `json:"sup"`
}

func (c *fileConfig) Raw() ([]byte, error) {
	return json.Marshal(c)
}

func NewFile(ref string) (v1.OCICore, error) {
	var getter v1.Getter
	if strings.HasPrefix(ref, "http") || strings.HasPrefix(ref, "https") {
		getter = remoteGetter(ref)
	} else {
		getter = localFileGetter(ref)
	}

	var layers []gv1.Layer
	layer, err := newLayer(getter)
	if err != nil {
		return nil, err
	}

	layers = append(layers, layer)

	c, err := v1.Core(types.UnknownManifest, &fileConfig{}, layers)
	if err != nil {
		return nil, err
	}

	return &file{
		OCICore: c,
	}, nil
}

type layer struct {
	*v1.Layer
}

func (l *layer) MediaType() (types.MediaType, error) {
	return LayerMediaType, nil
}

func newLayer(getter v1.Getter) (gv1.Layer, error) {
	ll, err := v1.NewLayer(getter)
	if err != nil {
		return nil, err
	}
	return ll, nil
}

func localFileGetter(path string) v1.Getter {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

func remoteGetter(url string) v1.Getter {
	return func() (io.ReadCloser, error) {
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
}
