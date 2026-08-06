package content

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"

	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// BuildTransport returns a RoundTripper honoring insecureSkipTLSVerify and caFile.
// insecureSkipTLSVerify takes precedence: when set, caFile is ignored.
func BuildTransport(insecureSkipTLSVerify bool, caFile string) (http.RoundTripper, error) {
	if !insecureSkipTLSVerify && caFile == "" {
		return remote.DefaultTransport, nil
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}

	if insecureSkipTLSVerify {
		tr.TLSClientConfig.InsecureSkipVerify = true
		return tr, nil
	}

	// caFile path (only reached when insecureSkipTLSVerify is false)
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
	tr.TLSClientConfig.RootCAs = pool
	return tr, nil
}
