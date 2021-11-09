package chart

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/artifact/local"
	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

const (
	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
)

var _ artifact.OCI = (*chrt)(nil)

type chrt struct {
	path string

	annotations map[string]string
}

func NewChart(name, repo, version string) (artifact.OCI, error) {
	cpo := action.ChartPathOptions{
		RepoURL: repo,
		Version: version,
	}

	cp, err := cpo.LocateChart(name, cli.New())
	if err != nil {
		return nil, err
	}

	return &chrt{
		path: cp,
	}, nil
}

func (h *chrt) MediaType() string {
	return types.OCIManifestSchema1
}

func (h *chrt) Manifest() (*gv1.Manifest, error) {
	cfgDesc, err := h.configDescriptor()
	if err != nil {
		return nil, err
	}

	var layerDescs []gv1.Descriptor
	ls, err := h.Layers()
	for _, l := range ls {
		desc, err := partial.Descriptor(l)
		if err != nil {
			return nil, err
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

func (h *chrt) RawConfig() ([]byte, error) {
	ch, err := loader.Load(h.path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ch.Metadata)
}

func (h *chrt) configDescriptor() (gv1.Descriptor, error) {
	data, err := h.RawConfig()
	if err != nil {
		return gv1.Descriptor{}, err
	}

	hash, size, err := gv1.SHA256(bytes.NewBuffer(data))
	if err != nil {
		return gv1.Descriptor{}, err
	}

	return gv1.Descriptor{
		MediaType: types.ChartConfigMediaType,
		Size:      size,
		Digest:    hash,
	}, nil
}

func (h *chrt) Layers() ([]gv1.Layer, error) {
	chartDataLayer, err := h.chartDataLayer()
	if err != nil {
		return nil, err
	}

	return []gv1.Layer{
		chartDataLayer,
		// TODO: Add provenance
	}, nil
}

func (h *chrt) RawChartData() ([]byte, error) {
	return os.ReadFile(h.path)
}

func (h *chrt) chartDataLayer() (gv1.Layer, error) {
	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = filepath.Base(h.path)

	return local.LayerFromOpener(chartOpener(h.path),
		local.WithMediaType(types.ChartLayerMediaType),
		local.WithAnnotations(annotations))
}

func chartOpener(path string) local.Opener {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}
