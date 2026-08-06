package store

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hauler.dev/go/hauler/v2/pkg/content"
)

func transport(t *testing.T, rt http.RoundTripper) *http.Transport {
	t.Helper()
	tr, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", rt)
	}
	return tr
}

// insecureSkipTLSVerify must win over caFile: a bogus caFile is never read.
func TestBuildTransport_InsecurePrecedence(t *testing.T) {
	rt, err := content.BuildTransport(true, "/definitely/not/here.pem")
	if err != nil {
		t.Fatalf("want no error (caFile must be ignored when insecure), got %v", err)
	}
	if !transport(t, rt).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify not set")
	}
}

func TestBuildTransport_CAFileErrors(t *testing.T) {
	if _, err := content.BuildTransport(false, "/definitely/not/here.pem"); err == nil {
		t.Fatal("missing caFile: want error, got nil")
	}
	junk := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(junk, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := content.BuildTransport(false, junk); err == nil {
		t.Fatal("junk caFile: want error, got nil")
	}
}

func TestBuildTransport_Noop(t *testing.T) {
	if _, err := content.BuildTransport(false, ""); err != nil {
		t.Fatalf("want no error, got %v", err)
	}
}

// Real TLS handshakes: caFile trusts a matching server, rejects an unrelated CA,
// and insecure trusts anything.
func TestBuildTransport_TLSHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	caFile := writeCertPEM(t, srv.Certificate().Raw)
	rt, err := content.BuildTransport(false, caFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&http.Client{Transport: rt}).Get(srv.URL); err != nil {
		t.Fatalf("matching caFile: want success, got %v", err)
	}

	rt, _ = content.BuildTransport(false, unrelatedCAFile(t))
	if _, err := (&http.Client{Transport: rt}).Get(srv.URL); err == nil {
		t.Fatal("unrelated caFile: want TLS error, got success")
	}

	rt, _ = content.BuildTransport(true, "")
	if _, err := (&http.Client{Transport: rt}).Get(srv.URL); err != nil {
		t.Fatalf("insecure: want success, got %v", err)
	}
}

func writeCertPEM(t *testing.T, der []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func unrelatedCAFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "unrelated"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return writeCertPEM(t, der)
}
