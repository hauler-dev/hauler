package git

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing/transport"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

// writeTestPEM writes one pem block to name under a temp dir and returns its path.
func writeTestPEM(t *testing.T, name, blockType string, der []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// writeTestClientCert writes a self-signed client certificate and key, returning both paths.
func writeTestClientCert(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshaling key: %v", err)
	}
	return writeTestPEM(t, "client.crt", "CERTIFICATE", certDER), writeTestPEM(t, "client.key", "EC PRIVATE KEY", keyDER)
}

// probeGitHTTPS asks the installed https client for url's refs, so the error shows whether tls was trusted.
func probeGitHTTPS(t *testing.T, url string) error {
	t.Helper()
	ep, err := transport.NewEndpoint(url)
	if err != nil {
		t.Fatalf("parsing endpoint: %v", err)
	}
	c, err := gitclient.NewClient(ep)
	if err != nil {
		t.Fatalf("building client: %v", err)
	}
	s, err := c.NewUploadPackSession(ep, nil)
	if err != nil {
		t.Fatalf("opening session: %v", err)
	}
	_, err = s.AdvertisedReferences()
	return err
}

// A client certificate must not make the clone ignore the ca file, and insecure must still win without reading it.
func TestInstallGitHTTPClientClientCertUsesCaFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	t.Cleanup(func() { gitclient.InstallProtocol("https", githttp.DefaultClient) })

	certFile, keyFile := writeTestClientCert(t)
	caFile := writeTestPEM(t, "ca.pem", "CERTIFICATE", srv.Certificate().Raw)
	url := srv.URL + "/repo.git"

	if err := installGitHTTPClient(cloneAuth{certFile: certFile, keyFile: keyFile}); err != nil {
		t.Fatalf("installGitHTTPClient without ca: %v", err)
	}
	if err := probeGitHTTPS(t, url); err == nil || errors.Is(err, transport.ErrRepositoryNotFound) {
		t.Fatalf("control failed: the test server was trusted without its ca: %v", err)
	}

	if err := installGitHTTPClient(cloneAuth{certFile: certFile, keyFile: keyFile, caFile: caFile}); err != nil {
		t.Fatalf("installGitHTTPClient with ca: %v", err)
	}
	if err := probeGitHTTPS(t, url); !errors.Is(err, transport.ErrRepositoryNotFound) {
		t.Fatalf("ca file was ignored alongside a client certificate: %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing.pem")
	if err := installGitHTTPClient(cloneAuth{certFile: certFile, keyFile: keyFile, caFile: missing}); err == nil {
		t.Fatal("installGitHTTPClient accepted a missing ca file")
	}
	if err := installGitHTTPClient(cloneAuth{certFile: certFile, keyFile: keyFile, caFile: missing, insecureSkipTLSVerify: true}); err != nil {
		t.Fatalf("insecure did not win over a missing ca file: %v", err)
	}
	if err := probeGitHTTPS(t, url); !errors.Is(err, transport.ErrRepositoryNotFound) {
		t.Fatalf("insecure did not skip verification: %v", err)
	}
}
