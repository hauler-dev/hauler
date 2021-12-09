package chart

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/rancherfederal/hauler/internal/layer"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/consts"
)

var _ artifact.OCI = (*Chart)(nil)

// Chart implements the  OCI interface for Chart API objects. API spec values are
// stored into the Repo, Name, and Version fields.
type Chart struct {
	Repo    string
	Name    string
	Version string

	path string

	annotations map[string]string
}

func NewChart(name, repo, version string) (*Chart, error) {
	cpo := action.ChartPathOptions{
		RepoURL: repo,
		Version: version,
	}

	cp, err := cpo.LocateChart(name, cli.New())
	if err != nil {
		return nil, fmt.Errorf("locate chart: %w", err)
	}

	return &Chart{
		Repo:    repo,
		Name:    name,
		Version: version,
		path:    cp,
	}, nil
}

func (h *Chart) MediaType() string {
	return consts.OCIManifestSchema1
}

func (h *Chart) Manifest() (*gv1.Manifest, error) {
	cfgDesc, err := h.configDescriptor()
	if err != nil {
		return nil, fmt.Errorf("config descriptor: %w", err)
	}

	var layerDescs []gv1.Descriptor
	ls, err := h.Layers()
	for _, l := range ls {
		desc, err := partial.Descriptor(l)
		if err != nil {
			return nil, fmt.Errorf("layer descriptor: %w", err)
		}
		layerDescs = append(layerDescs, *desc)
	}

	return &gv1.Manifest{
		SchemaVersion: 2,
		MediaType:     gtypes.MediaType(h.MediaType()),
		Config:        cfgDesc,
		Layers:        layerDescs,
		Annotations:   h.annotations,
	}, nil
}

func (h *Chart) RawConfig() ([]byte, error) {
	ch, err := loader.Load(h.path)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	return json.Marshal(ch.Metadata)
}

func (h *Chart) configDescriptor() (gv1.Descriptor, error) {
	data, err := h.RawConfig()
	if err != nil {
		return gv1.Descriptor{}, fmt.Errorf("raw config: %w", err)
	}

	hash, size, err := gv1.SHA256(bytes.NewBuffer(data))
	if err != nil {
		return gv1.Descriptor{}, fmt.Errorf("hash: %w", err)
	}

	return gv1.Descriptor{
		MediaType: consts.ChartConfigMediaType,
		Size:      size,
		Digest:    hash,
	}, nil
}

func (h *Chart) Load() (*chart.Chart, error) {
	rc, err := chartOpener(h.path)()
	if err != nil {
		return nil, fmt.Errorf("open chart: %w", err)
	}
	defer rc.Close()
	return loader.LoadArchive(rc)
}

func (h *Chart) Layers() ([]gv1.Layer, error) {
	chartDataLayer, err := h.chartDataLayer()
	if err != nil {
		return nil, fmt.Errorf("chart data layer: %w", err)
	}

	return []gv1.Layer{
		chartDataLayer,
		// TODO: Add provenance
	}, nil
}

func (h *Chart) RawChartData() ([]byte, error) {
	return os.ReadFile(h.path)
}

func (h *Chart) chartDataLayer() (gv1.Layer, error) {
	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = filepath.Base(h.path)

	return layer.FromOpener(chartOpener(h.path),
		layer.WithMediaType(consts.ChartLayerMediaType),
		layer.WithAnnotations(annotations))
}

func chartOpener(path string) layer.Opener {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}
