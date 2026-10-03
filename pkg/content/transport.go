package content

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rs/zerolog"
)

type transport struct {
	base http.RoundTripper
}

// plainHTTPWarned records the hosts already warned about, so each host warns once per process instead of once per transport.
var plainHTTPWarned sync.Map

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.EqualFold(req.URL.Scheme, "http") {
		if _, warned := plainHTTPWarned.LoadOrStore(req.URL.Host, struct{}{}); !warned {
			zerolog.Ctx(req.Context()).Warn().Msgf("pulling content over plain HTTP [%s]", req.URL.Host)
		}
	}

	return t.base.RoundTrip(req)
}

// CAPool returns the system roots plus the certificates in caFile, the trust every --ca-file flag uses.
func CAPool(caFile string) (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("reading CA file %q: %w", caFile, err)
	}

	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no valid certificates found in CA file %q", caFile)
	}

	return pool, nil
}

// BuildTransport returns a RoundTripper configured with the requested TLS
// settings. It also warns if the image pull makes a request over plain HTTP.
//
// insecureSkipTLSVerify takes precedence over caFile. When enabled, caFile is
// ignored.
func BuildTransport(insecureSkipTLSVerify bool, caFile string) (http.RoundTripper, error) {
	base := remote.DefaultTransport

	if insecureSkipTLSVerify || caFile != "" {
		defaultTransport, ok := remote.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("unexpected default transport type %T", remote.DefaultTransport)
		}

		tr := defaultTransport.Clone()

		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		} else {
			tr.TLSClientConfig = tr.TLSClientConfig.Clone()
		}

		if insecureSkipTLSVerify {
			tr.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec
		} else {
			pool, err := CAPool(caFile)
			if err != nil {
				return nil, err
			}

			tr.TLSClientConfig.RootCAs = pool
		}

		base = tr
	}

	return &transport{
		base: base,
	}, nil
}
