package directory

import (
	"context"
	"fmt"
	"os"
	"strings"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/getter"
)

// interface guard
var _ artifacts.OCI = (*Directory)(nil)

// Directory implements the OCI interface for a local directory tree, stored as one archive that extracts back into the tree.
type Directory struct {
	Path string

	computed bool
	client   *getter.Client
	config   artifacts.Config
	blob     gv1.Layer
	manifest *gv1.Manifest
	ctx      context.Context
}

// NewDirectory returns a Directory for path, or an error if path isn't a real directory or yields an unsafe name.
func NewDirectory(path string, opts ...Option) (*Directory, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("unable to access [%s]: %w", path, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("[%s] is not a directory (use `store add file` for a single file)", path)
	}

	d := &Directory{
		client: getter.NewClient(getter.ClientOptions{}),
		Path:   path,
		ctx:    context.Background(),
	}
	for _, opt := range opts {
		opt(d)
	}

	// Reject names that would extract onto or outside the destination instead of into a subdirectory of it.
	if name := d.Name(path); name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return nil, fmt.Errorf("invalid directory name [%s]", name)
	}
	return d, nil
}

// Name is the name of the directory's reference.
func (d *Directory) Name(path string) string {
	return d.client.Name(path)
}

func (d *Directory) MediaType() string {
	return consts.OCIManifestSchema1
}

func (d *Directory) RawConfig() ([]byte, error) {
	if err := d.compute(); err != nil {
		return nil, err
	}
	return d.config.Raw()
}

func (d *Directory) Layers() ([]gv1.Layer, error) {
	if err := d.compute(); err != nil {
		return nil, err
	}
	return []gv1.Layer{d.blob}, nil
}

func (d *Directory) Manifest() (*gv1.Manifest, error) {
	if err := d.compute(); err != nil {
		return nil, err
	}
	return d.manifest, nil
}

// Size returns the compressed byte size of the directory's single archive layer.
func (d *Directory) Size() (int64, error) {
	if err := d.compute(); err != nil {
		return 0, err
	}
	return d.blob.Size()
}

func (d *Directory) compute() error {
	if d.computed {
		return nil
	}

	blob, err := d.client.LayerFrom(d.ctx, d.Path)
	if err != nil {
		return err
	}

	layer, err := partial.Descriptor(blob)
	if err != nil {
		return err
	}

	cfg := d.client.Config(d.Path)
	cfgDesc, err := partial.Descriptor(cfg)
	if err != nil {
		return err
	}

	d.manifest = &gv1.Manifest{
		SchemaVersion: 2,
		MediaType:     gtypes.MediaType(d.MediaType()),
		Config:        *cfgDesc,
		Layers:        []gv1.Descriptor{*layer},
	}
	d.config = cfg
	d.blob = blob
	d.computed = true
	return nil
}
