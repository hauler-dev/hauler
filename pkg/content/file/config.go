package file

import (
	"bytes"
	"encoding/json"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

var _ partial.Describable = (*config)(nil)

type config struct {
	Reference   string            `json:"ref"`  // Reference is the reference from where the file was sourced
	Name        string            `json:"name"` // Name is the files name on disk
	Annotations map[string]string `json:"annotations,omitempty"`
	URLs        []string          `json:"urls,omitempty"`

	computed bool
	size     int64
	hash     gv1.Hash
}

func (c config) Descriptor() (gv1.Descriptor, error) {
	if err := c.compute(); err != nil {
		return gv1.Descriptor{}, err
	}

	return gv1.Descriptor{
		MediaType:   types.FileMediaType,
		Size:        c.size,
		Digest:      c.hash,
		URLs:        c.URLs,
		Annotations: c.Annotations,
		// Platform:    nil,
	}, nil
}

func (c config) Digest() (gv1.Hash, error) {
	if err := c.compute(); err != nil {
		return gv1.Hash{}, err
	}
	return c.hash, nil
}

func (c config) MediaType() (gtypes.MediaType, error) {
	return types.FileMediaType, nil
}

func (c config) Size() (int64, error) {
	if err := c.compute(); err != nil {
		return 0, err
	}
	return c.size, nil
}

func (c *config) Raw() ([]byte, error) {
	return json.Marshal(c)
}

func (c *config) compute() error {
	if c.computed {
		return nil
	}

	data, err := c.Raw()
	if err != nil {
		return err
	}

	h, size, err := gv1.SHA256(bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	c.size = size
	c.hash = h
	return nil
}
