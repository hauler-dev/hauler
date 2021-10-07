package content

import (
	"context"
	"encoding/json"
	"os"

	"github.com/containerd/containerd/remotes/docker"
	"github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	hchart "helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"oras.land/oras-go/pkg/content"
	"oras.land/oras-go/pkg/oras"

	"github.com/rancherfederal/hauler/pkg/log"
)

// NOTE: Most of this is "inspired" (copied) from helm
// TODO: Migrate to helm libraries once it's out of experimental

const (
	// ConfigMediaType is the reserved media type for the Helm chart manifest config
	ConfigMediaType = "application/vnd.cncf.helm.config.v1+json"

	// ChartLayerMediaType is the reserved media type for Helm chart package content
	ChartLayerMediaType = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
)

type chart struct {
	downloader downloader.ChartDownloader
	reference  string
	chartRef   string

	helmChart *hchart.Chart
}

func NewChart(reference string, chartRef string) *chart {
	return &chart{
		downloader: downloader.ChartDownloader{
			Out:     nil,
			Verify:  downloader.VerifyNever,
			Getters: getter.All(cli.New()),
			Options: []getter.Option{
				getter.WithInsecureSkipVerifyTLS(true),
			},
		},

		reference: reference,
		chartRef:  chartRef,
	}
}

// TODO: We could just use this to wrap NewGeneric, but leaving it separate for when we eventually move to helm's stable lib
func (o chart) Relocate(ctx context.Context, registry string, option ...Option) error {
	l := log.FromContext(ctx).With(log.Fields{
		"content": "chart",
	})

	// TODO: We need this because we're using a filesystem store, evaluate if we can use a memorystore, or some hybrid
	tmpdir, err := os.MkdirTemp("", "hauler-generic-relocate")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)

	store := content.NewFileStore(tmpdir)
	defer store.Close()

	l.Debugf("Downloading chart from: %s", o.chartRef)
	chartPath, _, err := o.downloader.DownloadTo(o.chartRef, "", tmpdir)
	if err != nil {
		return err
	}

	l.Debugf("Downloaded chart to %s", chartPath)

	loadedChart, err := loader.Load(chartPath)
	if err != nil {
		return err
	}

	chartDescriptor, err := store.Add("", ChartLayerMediaType, chartPath)
	if err != nil {
		return err
	}

	configData, err := json.Marshal(loadedChart.Metadata)
	if err != nil {
		return err
	}

	configDescriptor, err := writeToFileStore(store, "config", ConfigMediaType, configData)

	descriptors := []ocispec.Descriptor{chartDescriptor}

	resolver := docker.NewResolver(docker.ResolverOptions{})

	rRef, err := name.ParseReference(o.reference, name.WithDefaultRegistry(registry))
	if err != nil {
		return err
	}

	l.Debugf("Relocating chart from '%s' --> '%s'", o.reference, rRef.Name())
	_, err = oras.Push(ctx, resolver, rRef.Name(), store, descriptors,
		oras.WithConfig(configDescriptor), oras.WithNameValidation(nil))

	return err
}

func (o chart) Remove(ctx context.Context, reference string) error {
	return nil
}
