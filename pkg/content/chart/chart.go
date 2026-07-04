package chart

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/layer"
)

var (
	_        artifacts.OCI = (*Chart)(nil)
	settings               = cli.New()
)

// chart implements the oci interface for chart api objects... api spec values are stored into the name, repo, and version fields
type Chart struct {
	path        string
	annotations map[string]string
}

// newchart is a helper method that returns newlocalchart or newremotechart depending on chart contents
func NewChart(name string, opts *action.ChartPathOptions) (*Chart, error) {
	chartRef := name
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), settings.Namespace(), os.Getenv("HELM_DRIVER")); err != nil {
		return nil, err
	}

	client := action.NewInstall(actionConfig)

	// Propagate auth, TLS, and verification options from the caller.
	// RepoURL is intentionally NOT copied here — it is set conditionally below
	// based on URL scheme (OCI vs HTTP vs bare).
	client.ChartPathOptions.Version = opts.Version
	client.ChartPathOptions.Verify = opts.Verify
	client.ChartPathOptions.Keyring = opts.Keyring
	client.ChartPathOptions.Username = opts.Username
	client.ChartPathOptions.Password = opts.Password
	client.ChartPathOptions.PassCredentialsAll = opts.PassCredentialsAll
	client.ChartPathOptions.CertFile = opts.CertFile
	client.ChartPathOptions.KeyFile = opts.KeyFile
	client.ChartPathOptions.CaFile = opts.CaFile
	client.ChartPathOptions.InsecureSkipTLSVerify = opts.InsecureSkipTLSVerify
	client.ChartPathOptions.PlainHTTP = opts.PlainHTTP

	registryClient, err := newRegistryClient(client.CertFile, client.KeyFile, client.CaFile,
		client.InsecureSkipTLSVerify, client.PlainHTTP)
	if err != nil {
		return nil, fmt.Errorf("missing registry client: %w", err)
	}

	client.SetRegistryClient(registryClient)
	if registry.IsOCI(opts.RepoURL) {
		chartRef = opts.RepoURL + "/" + name
	} else if isUrl(opts.RepoURL) { // oci protocol registers as a valid url
		client.ChartPathOptions.RepoURL = opts.RepoURL
	} else { // handles cases like grafana and loki
		chartRef = opts.RepoURL + "/" + name
	}

	// suppress helm downloader oci logs (stdout/stderr)
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	chartPath, err := client.ChartPathOptions.LocateChart(chartRef, settings)

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_, _ = io.Copy(io.Discard, rOut)
	_, _ = io.Copy(io.Discard, rErr)
	rOut.Close()
	rErr.Close()

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

func (h *Chart) Load() (*v2.Chart, error) {
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

// chartdata loads the chart contents into memory and returns a NopCloser for the contents
// normally we avoid loading into memory, but charts sizes are strictly capped at ~1MB
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

	// title defaults to the downloaded file's basename. Helm v4's
	// ChartPathOptions.LocateChart downloads any non-local chart (HTTP repo or
	// OCI) into a content-addressed cache and returns a hash-named path (e.g.
	// "<sha256hex>.chart", using the literal extension defined by
	// downloader.CacheChart) rather than a human-readable filename, so
	// filepath.Base(h.path) is meaningless for those sources. In that specific
	// case only, prefer the canonical "<name>-<version>.tgz" form derived from
	// the chart's own Chart.yaml metadata. Genuinely local archives (".tgz" or
	// any other extension a user's file might carry) keep their real filename,
	// even if it doesn't follow the "<name>-<version>.tgz" convention.
	title := filepath.Base(h.path)
	if !info.IsDir() && filepath.Ext(h.path) == ".chart" {
		if ch, err := loader.Load(h.path); err == nil && ch.Metadata != nil && ch.Metadata.Name != "" && ch.Metadata.Version != "" {
			title = fmt.Sprintf("%s-%s.tgz", ch.Metadata.Name, ch.Metadata.Version)
		}
	}

	annotations := make(map[string]string)
	annotations[ocispec.AnnotationTitle] = title

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
		registry.ClientOptWriter(io.Discard),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
	}
	if plainHTTP {
		opts = append(opts, registry.ClientOptPlainHTTP())
	}

	// create a new registry client
	registryClient, err := registry.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return registryClient, nil
}

// newRegistryClientWithTLS builds a registry client backed by an HTTP client with a custom
// TLS config. Helm v4 removed the registry.NewRegistryClientWithTLS convenience wrapper (it
// delegated to helm's internal/tlsutil package, which is not importable outside the helm
// module), so the TLS config construction is inlined here to match its prior behavior.
func newRegistryClientWithTLS(certFile, keyFile, caFile string, insecureSkipTLSverify bool) (*registry.Client, error) {
	tlsConf, err := newTLSConfig(certFile, keyFile, caFile, insecureSkipTLSverify)
	if err != nil {
		return nil, fmt.Errorf("can't create TLS config for client: %w", err)
	}

	registryClient, err := registry.NewClient(
		registry.ClientOptDebug(settings.Debug),
		registry.ClientOptEnableCache(true),
		registry.ClientOptWriter(io.Discard),
		registry.ClientOptCredentialsFile(settings.RegistryConfig),
		registry.ClientOptHTTPClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConf,
				Proxy:           http.ProxyFromEnvironment,
			},
		}),
	)
	if err != nil {
		return nil, err
	}
	return registryClient, nil
}

// newTLSConfig constructs a *tls.Config from the given cert/key/CA files, mirroring the
// behavior of helm's internal tlsutil.NewTLSConfig.
func newTLSConfig(certFile, keyFile, caFile string, insecureSkipTLSverify bool) (*tls.Config, error) {
	config := &tls.Config{
		InsecureSkipVerify: insecureSkipTLSverify,
	}

	if certFile != "" && keyFile != "" {
		certPEMBlock, err := os.ReadFile(certFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read cert file: %q: %w", certFile, err)
		}
		keyPEMBlock, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read key file: %q: %w", keyFile, err)
		}
		cert, err := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
		if err != nil {
			return nil, fmt.Errorf("unable to load cert from key pair: %w", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}

	if caFile != "" {
		caPEMBlock, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("can't read CA file: %q: %w", caFile, err)
		}
		cp := x509.NewCertPool()
		if !cp.AppendCertsFromPEM(caPEMBlock) {
			return nil, fmt.Errorf("failed to append certificates from pem block")
		}
		config.RootCAs = cp
	}

	return config, nil
}

// path returns the local filesystem path to the chart archive or directory
func (h *Chart) Path() string {
	return h.path
}
