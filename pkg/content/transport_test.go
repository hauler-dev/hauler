package content

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// writeTestCA writes cert as a pem ca file and returns its path.
func writeTestCA(t *testing.T, cert *x509.Certificate) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0o600); err != nil {
		t.Fatalf("writing ca file: %v", err)
	}
	return path
}

// A ca file must add to the system roots, never replace them.
func TestCAPoolAddsToSystemRoots(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()

	pool, err := CAPool(writeTestCA(t, srv.Certificate()))
	if err != nil {
		t.Fatalf("CAPool: %v", err)
	}

	system, err := x509.SystemCertPool()
	if err != nil || system == nil {
		system = x509.NewCertPool()
	}
	want := system.Clone()
	want.AddCert(srv.Certificate())
	if !pool.Equal(want) {
		t.Fatal("CAPool is not the system roots plus the ca file")
	}

	caOnly := x509.NewCertPool()
	caOnly.AddCert(srv.Certificate())
	if !system.Equal(x509.NewCertPool()) && pool.Equal(caOnly) {
		t.Fatal("CAPool dropped the system roots")
	}
}

// A missing or invalid ca file must fail instead of silently trusting only the system roots.
func TestCAPoolRejectsBadCaFile(t *testing.T) {
	if _, err := CAPool(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("CAPool accepted a missing ca file")
	}

	junk := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("writing junk file: %v", err)
	}
	if _, err := CAPool(junk); err == nil {
		t.Fatal("CAPool accepted a ca file with no certificates")
	}
}

// The plain http warning must fire once per host for the whole process, however many transports reach that host.
func TestPlainHTTPWarnsOncePerHost(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).WithContext(context.Background())

	hit := func(url string) {
		t.Helper()
		rt, err := BuildTransport(false, "")
		if err != nil {
			t.Fatalf("BuildTransport: %v", err)
		}
		for range 2 {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				t.Fatalf("building request: %v", err)
			}
			resp, err := rt.RoundTrip(req)
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			resp.Body.Close()
		}
	}
	warnings := func() int {
		return strings.Count(buf.String(), "pulling content over plain HTTP")
	}

	first := httptest.NewServer(http.NotFoundHandler())
	defer first.Close()
	hit(first.URL)
	hit(first.URL)
	if got := warnings(); got != 1 {
		t.Fatalf("two transports to one host logged %d warnings, want 1", got)
	}

	second := httptest.NewServer(http.NotFoundHandler())
	defer second.Close()
	hit(second.URL)
	if got := warnings(); got != 2 {
		t.Fatalf("a second host brought the total to %d warnings, want 2", got)
	}
}
