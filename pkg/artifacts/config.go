package artifacts

import (
	"bytes"
	"encoding/json"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"hauler.dev/hauler/pkg/consts"
)

var _ partial.Describable = (*marshallableConfig)(nil)

type Config interface {
	// Raw returns the config bytes
	Raw() ([]byte, error)

	Digest() (v1.Hash, error)

	MediaType() (types.MediaType, error)

	Size() (int64, error)
}

type Marshallable interface{}

type ConfigOption func(*marshallableConfig)

// ToConfig takes anything that is marshallabe and converts it into a Config
func ToConfig(i Marshallable, opts ...ConfigOption) Config {
	mc := &marshallableConfig{Marshallable: i}
	for _, o := range opts {
		o(mc)
	}
	return mc
}

func WithConfigMediaType(mediaType string) ConfigOption {
	return func(config *marshallableConfig) {
		config.mediaType = mediaType
	}
}

// marshallableConfig implements Config using helper methods
type marshallableConfig struct {
	Marshallable

	mediaType string
}

func (c *marshallableConfig) MediaType() (types.MediaType, error) {
	mt := c.mediaType
	if mt == "" {
		mt = consts.UnknownManifest
	}
	return types.MediaType(mt), nil
}

func (c *marshallableConfig) Raw() ([]byte, error) {
	return json.Marshal(c.Marshallable)
}

func (c *marshallableConfig) Digest() (v1.Hash, error) {
	return Digest(c)
}

func (c *marshallableConfig) Size() (int64, error) {
	return Size(c)
}

type WithRawConfig interface {
	Raw() ([]byte, error)
}

func Digest(c WithRawConfig) (v1.Hash, error) {
	b, err := c.Raw()
	if err != nil {
		return v1.Hash{}, err
	}
	digest, _, err := v1.SHA256(bytes.NewReader(b))
	return digest, err
}

func Size(c WithRawConfig) (int64, error) {
	b, err := c.Raw()
	if err != nil {
		return -1, err
	}
	return int64(len(b)), nil
}
