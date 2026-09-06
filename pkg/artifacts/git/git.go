package git

import (
	"context"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/getter"
)

// interface guard
var _ artifacts.OCI = (*Git)(nil)

// Git implements the OCI interface for a git repository, mirroring pkg/artifacts/file.File but with its own config media type so `store serve git` can find it later. Path may be a local bare repo directory or a remote clone URL, since compute clones the latter with go-git before storing it the same way, the same shape pkg/artifacts/chart.Chart uses for its own remote fetch.
type Git struct {
	Path string

	computed bool
	client   *getter.Client
	config   artifacts.Config
	blob     gv1.Layer
	manifest *gv1.Manifest
	ctx      context.Context

	nameOverride string
	auth         cloneAuth

	// cloneCleanup removes a URL's temp clone directory; deferred to Close since client.LayerFrom's opener reads it lazily, after compute returns.
	cloneCleanup func()
}

type gitConfig struct {
	Reference string `json:"reference"`
}

func NewGit(path string, opts ...Option) *Git {
	g := &Git{
		client: getter.NewClient(getter.ClientOptions{}),
		Path:   path,
		ctx:    context.Background(),
	}

	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Name is the name of the git repository's reference.
func (g *Git) Name(path string) string {
	if g.nameOverride != "" {
		return g.nameOverride
	}
	if IsGitURL(path) {
		return deriveGitName(path)
	}
	return g.client.Name(path)
}

func (g *Git) MediaType() string {
	return consts.OCIManifestSchema1
}

func (g *Git) RawConfig() ([]byte, error) {
	if err := g.compute(); err != nil {
		return nil, err
	}
	return g.config.Raw()
}

func (g *Git) Layers() ([]gv1.Layer, error) {
	if err := g.compute(); err != nil {
		return nil, err
	}
	return []gv1.Layer{g.blob}, nil
}

func (g *Git) Manifest() (*gv1.Manifest, error) {
	if err := g.compute(); err != nil {
		return nil, err
	}
	return g.manifest, nil
}

// Close removes the temp directory a URL was cloned into, if compute cloned one. Callers that construct a Git from a URL should defer this once they're done reading its Layers/Manifest/RawConfig, since calling it any earlier can delete the clone before those are actually read, because the layer's opener is lazy.
func (g *Git) Close() error {
	if g.cloneCleanup != nil {
		g.cloneCleanup()
		g.cloneCleanup = nil
	}
	return nil
}

// Size returns the compressed byte size of the repository's single layer, computing the content if needed.
func (g *Git) Size() (int64, error) {
	layers, err := g.Layers()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, l := range layers {
		sz, err := l.Size()
		if err != nil {
			return 0, err
		}
		total += sz
	}
	return total, nil
}

func (g *Git) compute() error {
	if g.computed {
		return nil
	}

	path := g.Path
	switch {
	case IsGitURL(path):
		clonedDir, cleanup, err := g.clone(path)
		if err != nil {
			return err
		}
		g.cloneCleanup = cleanup
		path = clonedDir
	case IsNonBareRepo(path):
		// A normal (non-bare) local repo has its internals nested under .git; mirror it into a fresh bare clone the same way a remote URL is cloned, since go-git's local file transport clones from a plain filesystem path just as it does from a URL.
		clonedDir, cleanup, err := g.clone(path)
		if err != nil {
			return err
		}
		g.cloneCleanup = cleanup
		path = clonedDir
	}

	if err := ValidateBareRepo(path); err != nil {
		return err
	}

	blob, err := g.client.LayerFrom(g.ctx, path)
	if err != nil {
		return err
	}

	layer, err := partial.Descriptor(blob)
	if err != nil {
		return err
	}

	annotations := make(map[string]string, len(layer.Annotations)+1)
	for k, v := range layer.Annotations {
		annotations[k] = v
	}
	annotations[ocispec.AnnotationTitle] = g.Name(g.Path)
	layer.Annotations = annotations

	cfg := artifacts.ToConfig(gitConfig{Reference: g.Path}, artifacts.WithConfigMediaType(consts.GitRepoConfigMediaType))
	cfgDesc, err := partial.Descriptor(cfg)
	if err != nil {
		return err
	}

	g.manifest = &gv1.Manifest{
		SchemaVersion: 2,
		MediaType:     gtypes.MediaType(g.MediaType()),
		Config:        *cfgDesc,
		Layers:        []gv1.Descriptor{*layer},
	}
	g.config = cfg
	g.blob = blob
	g.computed = true
	return nil
}
