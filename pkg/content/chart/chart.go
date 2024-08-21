package chart

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"hauler.dev/hauler/pkg/artifacts"
	"hauler.dev/hauler/pkg/log"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"

	"hauler.dev/hauler/pkg/consts"
	"hauler.dev/hauler/pkg/layer"
)

var (
	_        artifacts.OCI = (*Chart)(nil)
	settings               = cli.New()
)

// Chart implements the  OCI interface for Chart API objects. API spec values are
// stored into the Repo, Name, and Version fields.
type Chart struct {
	path        string
	annotations map[string]string
}

// NewChart is a helper method that returns NewLocalChart or NewRemoteChart depending on v1alpha1.Chart contents
func NewChart(name string, opts *action.ChartPathOptions) (*Chart, error) {
	chartRef := name
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER"), log.NewLogger(os.Stdout).Debugf); err != nil {
		return nil, err
	}

	client := action.NewInstall(actionConfig)
	client.ChartPathOptions.Version = opts.Version

	registryClient, err := newRegistryClient(client.CertFile, client.KeyFile, client.CaFile,
		client.InsecureSkipTLSverify, client.PlainHTTP)
	if err != nil {
		return nil, fmt.Errorf("missing registry client: %w", err)
	}

	client.SetRegistryClient(registryClient)
	if registry.IsOCI(opts.RepoURL) {
		chartRef = opts.RepoURL + "/" + name
	} else if isUrl(opts.RepoURL) { // OCI Protocol registers as a valid URL
		client.ChartPathOptions.RepoURL = opts.RepoURL
	} else { // Handles cases like grafana/loki
		chartRef = opts.RepoURL + "/" + name
	}

	chartPath, err := client.ChartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, err
	}

	return &Chart{
		path: chartPath,
	}, err
}

func (h *Chart) MediaType() string {
	return consts.OCIManifestSchema1
}

func (h *Chart) Manifest() (*gv1.Manifest, error) {
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

func (h *Chart) RawConfig() ([]byte, error) {
	ch, err := loader.Load(h.path)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ch.Metadata)
}

func (h *Chart) configDescriptor() (gv1.Descriptor, error) {
	data, err := h.RawConfig()
	if err != nil {
		return gv1.Descriptor{}, err
	}

	hash, size, err := gv1.SHA256(bytes.NewBuffer(data))
	if err != nil {
		return gv1.Descriptor{}, err
	}

	return gv1.Descriptor{
		MediaType: consts.ChartConfigMediaType,
		Size:      size,
		Digest:    hash,
	}, nil
}

func (h *Chart) Load() (*chart.Chart, error) {
	return loader.Load(h.path)
}

func (h *Chart) Layers() ([]gv1.Layer, error) {
	chartDataLayer, err := h.chartData()
	if err != nil {
		return nil, err
	}

	return []gv1.Layer{
		chartDataLayer,
		// TODO: Add provenance
	}, nil
}

func (h *Chart) RawChartData() ([]byte, error) {
	return os.ReadFile(h.path)
}

// chartData loads the chart contents into memory and returns a NopCloser for the contents
//
//	Normally we avoid loading into memory, but charts sizes are strictly capped at ~1MB
func (h *Chart) chartData() (gv1.Layer, error) {
	info, err := os.Stat(h.path)
	if err != nil {
		return nil, err
	}

	var chartdata []byte
	if info.IsDir() {
		buf := &bytes.Buffer{}
		gw := gzip.NewWriter(buf)
		tw := tar.NewWriter(gw)

		if err := filepath.WalkDir(h.path, func(path string, d fs.DirEntry, err error) error {
			fi, err := d.Info()
			if err != nil {
				return err
			}

			header, err := tar.FileInfoHeader(fi, fi.Name())
			if err != nil {
				return err
			}

			rel, err := filepath.Rel(filepath.Dir(h.path), path)
			if err != nil {
				return err
			}
			header.Name = rel

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if !d.IsDir() {
				data, err := os.Open(path)
				if err != nil {
					return err
				}
				if _, err := io.Copy(tw, data); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			return nil, err
		}

		if err := tw.Close(); err != nil {
			return nil, err
		}
		if err := gw.Close(); err != nil {
			return nil, err
		}
		chartdata = buf.Bytes()

	} else {
		data, err := os.ReadFile(h.path)
		if err != nil {
			return nil, err
		}
		chartdata = data
	}

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = filepath.Base(h.path)

	opener := func() layer.Opener {
		return func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewBuffer(chartdata)), nil
		}
	}
	chartDataLayer, err := layer.FromOpener(opener(),
		layer.WithMediaType(consts.ChartLayerMediaType),
		layer.WithAnnotations(annotations))

	return chartDataLayer, err
}
func isUrl(name string) bool {
	_, err := url.ParseRequestURI(name)
	return err == nil
}

func newRegistryClient(certFile, keyFile, caFile string, insecureSkipTLSverify, plainHTTP bool) (*registry.Client, error) {
	if certFile != "" && keyFile != "" || caFile != "" || insecureSkipTLSverify {
		registryClient, err := newRegistryClientWithTLS(certFile, keyFile, caFile, insecureSkipTLSverify)
		if err != nil {
			return nil, err
		}
		return registryClient, nil
	}
	registryClient, err := newDefaultRegistryClient(plainHTTP)
	if err != nil {
		return nil, err
	}
	return registryClient, nil
}

func newDefaultRegistryClient(plainHTTP bool) (*registry.Client, error) {
	opts := []registry.ClientOption{
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(os.Stderr),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	}
	if plainHTTP {
		opts = append(opts, registry.ClientOptPlainHTTP())
	}

	// Create a new registry client
	registryClient, err := registry.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return registryClient, nil
}

func newRegistryClientWithTLS(certFile, keyFile, caFile string, insecureSkipTLSverify bool) (*registry.Client, error) {
	// Create a new registry client
	registryClient, err := registry.NewRegistryClientWithTLS(os.Stderr, certFile, keyFile, caFile, insecureSkipTLSverify,
		settings.RegistryConfig, settings.Debug,
	)
	if err != nil {
		return nil, err
	}
	return registryClient, nil
}
