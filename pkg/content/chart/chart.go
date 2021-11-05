package chart

import (
	"encoding/json"
	"io"
	"os"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/artifact/types"
)

const (
	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
)

type helmChart struct {
	artifact.OCI

	config *helmConfig
}

type helmConfig struct {
	get artifact.Getter
}

func (c *helmConfig) Raw() ([]byte, error) {
	rc, err := c.get()
	if err != nil {
		return nil, err
	}

	ch, err := loader.LoadArchive(rc)
	if err != nil {
		return nil, err
	}

	return json.Marshal(ch.Metadata)
}

func NewChart(name, repo, version string) (artifact.OCI, error) {
	cg := chartGetter(name, repo, version)
	chartDataLayer, err := newLayer(cg)
	if err != nil {
		return nil, err
	}

	var layers []gv1.Layer
	layers = append(layers, chartDataLayer)

	c, err := artifact.Core(types.UnknownManifest, &helmConfig{cg}, layers)
	if err != nil {
		return nil, err
	}

	return &helmChart{
		OCI: c,
	}, nil
}

type layer struct {
	*artifact.Layer
}

func (l *layer) MediaType() (gtypes.MediaType, error) {
	return ChartLayerMediaType, nil
}

func newLayer(getter artifact.Getter) (gv1.Layer, error) {
	ll, err := artifact.NewLayer(getter)
	if err != nil {
		return nil, err
	}
	return &layer{ll}, nil
}

func chartGetter(name, repoUrl, version string) artifact.Getter {
	return func() (io.ReadCloser, error) {
		cpo := action.ChartPathOptions{
			RepoURL: repoUrl,
			Version: version,
		}

		cp, err := cpo.LocateChart(name, cli.New())
		if err != nil {
			return nil, err
		}

		return os.Open(cp)
	}
}
