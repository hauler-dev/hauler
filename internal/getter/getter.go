package getter

import (
	"context"
	"io"
	"net/url"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"oras.land/oras-go/pkg/content"

	"github.com/rancherfederal/hauler/internal/layer"
	"github.com/rancherfederal/hauler/pkg/artifact"
	htypes "github.com/rancherfederal/hauler/pkg/consts"
)

type Client struct {
	Getters map[string]Getter
	Options ClientOptions
}

// TODO: Make some valid ClientOptions
type ClientOptions struct{}

var (
	ErrGetterTypeUnknown = errors.New("no getter type found matching reference")
)

type Getter interface {
	Open(context.Context, *url.URL) (io.ReadCloser, error)

	Detect(*url.URL) bool

	Name(*url.URL) string

	Config(*url.URL) artifact.Config
}

func NewClient(opts ClientOptions) *Client {
	defaults := map[string]Getter{
		"file":      new(file),
		"directory": new(directory),
		"http":      new(https),
		"https":     new(https),
	}

	c := &Client{
		Getters: defaults,
		Options: opts,
	}
	return c
}

func (c *Client) LayerFrom(ctx context.Context, source string) (v1.Layer, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	for _, g := range c.Getters {
		if g.Detect(u) {
			opener := func() (io.ReadCloser, error) {
				return g.Open(ctx, u)
			}
			cfg := g.Config(u)
			mt, err := cfg.MediaType()
			if err != nil {
				return nil, err
			}

			annotations := make(map[string]string)
			annotations[ocispec.AnnotationTitle] = g.Name(u)

			switch g.(type) {
			case *directory:
				annotations[content.AnnotationUnpack] = "true"
			}

			l, err := layer.FromOpener(opener,
				layer.WithMediaType(string(mt)),
				layer.WithAnnotations(annotations))
			if err != nil {
				return nil, err
			}
			return l, nil
		}
	}
	return nil, errors.Wrapf(ErrGetterTypeUnknown, "%s", source)
}

func (c *Client) Name(source string) string {
	u, err := url.Parse(source)
	if err != nil {
		return source
	}
	for _, g := range c.Getters {
		return g.Name(u)
	}
	return source
}

func (c *Client) Config(source string) artifact.Config {
	u, err := url.Parse(source)
	if err != nil {
		return nil
	}
	for _, g := range c.Getters {
		return g.Config(u)
	}
	return nil
}

type config struct {
	Reference   string            `json:"reference"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func (c *config) MediaType() (types.MediaType, error) {
	return htypes.FileConfigMediaType, nil
}
