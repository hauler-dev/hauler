package getter

import (
	"context"
	"fmt"
	"io"
	"net/url"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
	"oras.land/oras-go/pkg/content"

	content2 "github.com/rancherfederal/hauler/pkg/artifacts"
	"github.com/rancherfederal/hauler/pkg/consts"
	"github.com/rancherfederal/hauler/pkg/layer"
)

type Client struct {
	Getters map[string]Getter
	Options ClientOptions
}

// ClientOptions provides options for the client
type ClientOptions struct {
	NameOverride string
}

var (
	ErrGetterTypeUnknown = errors.New("no getter type found matching reference")
)

type Getter interface {
	Open(context.Context, *url.URL) (io.ReadCloser, error)

	Detect(*url.URL) bool

	Name(*url.URL) string

	Config(*url.URL) content2.Config
}

func NewClient(opts ClientOptions) *Client {
	defaults := map[string]Getter{
		"file":      NewFile(),
		"directory": NewDirectory(),
		"http":      NewHttp(),
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

	g, err := c.getterFrom(u)
	if err != nil {
		if errors.Is(err, ErrGetterTypeUnknown) {
			return nil, err
		}
		return nil, fmt.Errorf("create getter: %w", err)
	}

	opener := func() (io.ReadCloser, error) {
		return g.Open(ctx, u)
	}

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = c.Name(source)

	switch g.(type) {
	case *directory:
		annotations[content.AnnotationUnpack] = "true"
	}

	l, err := layer.FromOpener(opener,
		layer.WithMediaType(consts.FileLayerMediaType),
		layer.WithAnnotations(annotations))
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (c *Client) ContentFrom(ctx context.Context, source string) (io.ReadCloser, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse source %s: %w", source, err)
	}
	g, err := c.getterFrom(u)
	if err != nil {
		if errors.Is(err, ErrGetterTypeUnknown) {
			return nil, err
		}
		return nil, fmt.Errorf("create getter: %w", err)
	}
	return g.Open(ctx, u)
}

func (c *Client) getterFrom(srcUrl *url.URL) (Getter, error) {
	for _, g := range c.Getters {
		if g.Detect(srcUrl) {
			return g, nil
		}
	}
	return nil, errors.Wrapf(ErrGetterTypeUnknown, "source %s", srcUrl.String())
}

func (c *Client) Name(source string) string {
	if c.Options.NameOverride != "" {
		return c.Options.NameOverride
	}
	u, err := url.Parse(source)
	if err != nil {
		return source
	}
	for _, g := range c.Getters {
		if g.Detect(u) {
			return g.Name(u)
		}
	}
	return source
}

func (c *Client) Config(source string) content2.Config {
	u, err := url.Parse(source)
	if err != nil {
		return nil
	}
	for _, g := range c.Getters {
		if g.Detect(u) {
			return g.Config(u)
		}
	}
	return nil
}

type config struct {
	Reference   string            `json:"reference"`
	Annotations map[string]string `json:"annotations,omitempty"`
}
