package artifact

import (
	"bytes"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"k8s.io/apimachinery/pkg/util/json"
)

var _ partial.Describable = (*marshallableConfig)(nil)

type Config interface {
	// Raw returns the config bytes
	Raw() ([]byte, error)

	Digest() (v1.Hash, error)

	MediaType() (types.MediaType, error)

	Size() (int64, error)
}

type Marshallable interface {
	MediaType() (types.MediaType, error)
}

// ToConfig takes anything that is marshallabe and converts it into a Config
func ToConfig(i Marshallable) Config {
	return &marshallableConfig{
		Marshallable: i,
	}
}

// marshallableConfig implements Config using helper methods
type marshallableConfig struct {
	Marshallable

	hash v1.Hash
	size int64
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
