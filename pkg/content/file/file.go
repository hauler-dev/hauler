package file

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	gv1 "github.com/google/go-containerregistry/pkg/v1"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

var _ artifact.OCI = (*file)(nil)

type file struct {
	artifact.OCI
}

type fileConfig struct {
	Sup string `json:"sup"`

	MediaType   string            `json:"mediaType"`
	Annotations map[string]string `json:"annotations"`
}

func (c *fileConfig) Raw() ([]byte, error) {
	return json.Marshal(c)
}

func NewFile(ref string) (artifact.OCI, error) {
	var getter artifact.Getter
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

	c, err := artifact.Core(types.UnknownManifest, &fileConfig{
		Sup: "hi guys",
	}, layers)
	if err != nil {
		return nil, err
	}

	return &file{
		OCI: c,
	}, nil
}

type layer struct {
	*artifact.Layer
}

func (l *layer) MediaType() (string, error) {
	return types.FileLayerMediaType, nil
}

func newLayer(getter artifact.Getter) (gv1.Layer, error) {
	ll, err := artifact.NewLayer(getter)
	if err != nil {
		return nil, err
	}
	return ll, nil
}

func localFileGetter(path string) artifact.Getter {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

func remoteGetter(url string) artifact.Getter {
	return func() (io.ReadCloser, error) {
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
}
