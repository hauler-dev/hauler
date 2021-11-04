package chart

import (
	"io"
	"os"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	gmutate "github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/rancherfederal/hauler/pkg/artifact/v1"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/empty"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/mutate"
)

const (
	// OCIScheme is the URL scheme for OCI-based requests
	OCIScheme = "oci"

	// CredentialsFileBasename is the filename for auth credentials file
	CredentialsFileBasename = "config.json"

	// ConfigMediaType is the reserved media type for the Helm chart manifest config
	ConfigMediaType = "application/vnd.cncf.helm.config.v1+json"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"

	// ProvLayerMediaType is the reserved media type for Helm chart provenance files
	ProvLayerMediaType = "application/vnd.cncf.helm.chart.provenance.v1.prov"
)



func NewChart(name, repo, version string) (Chart, error) {
	cg := chartGetter(name, repo, version)
	chartDataLayer, err := newLayer(cg)
	if err != nil {
		return nil, err
	}

	base := mutate.MediaType(empty.Artifact, ConfigMediaType)
	base, err = mutate.Append(base, gmutate.Addendum{
		Layer: chartDataLayer,
		MediaType: ChartLayerMediaType,
	})
	if err != nil {
		return nil, err
	}

	// TODO: Handle charts provenance if it exists

	return base, nil
}

type layer struct {
	*v1.Layer
}

func (l *layer) MediaType() (types.MediaType, error) {
	return ChartLayerMediaType, nil
}

func newLayer(getter v1.Getter) (gv1.Layer, error) {
	ll, err := v1.NewLayer(getter)
	if err != nil {
		return nil, err
	}
	return &layer{ll}, nil
}

func chartGetter(name, repoUrl, version string) v1.Getter {
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
