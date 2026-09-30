package chart

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"helm.sh/helm/v4/pkg/action"
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

// The oci chart client must trust system roots plus the ca file, and insecure must win without reading it.
func TestNewTLSConfigCaFile(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	missing := filepath.Join(t.TempDir(), "missing.pem")

	cfg, err := newTLSConfig("", "", writeTestCA(t, srv.Certificate()), false)
	if err != nil {
		t.Fatalf("newTLSConfig: %v", err)
	}
	system, err := x509.SystemCertPool()
	if err != nil || system == nil {
		system = x509.NewCertPool()
	}
	want := system.Clone()
	want.AddCert(srv.Certificate())
	if !cfg.RootCAs.Equal(want) {
		t.Fatal("RootCAs is not the system roots plus the ca file")
	}

	if _, err := newTLSConfig("", "", missing, false); err == nil {
		t.Fatal("newTLSConfig accepted a missing ca file")
	}

	cfg, err = newTLSConfig("", "", missing, true)
	if err != nil {
		t.Fatalf("insecure did not win over a missing ca file: %v", err)
	}
	if !cfg.InsecureSkipVerify || cfg.RootCAs != nil {
		t.Fatal("insecure config still carries ca roots or verifies the server")
	}
}

// Helm's own https repo getter must not read the ca file when insecure is set.
func TestNewChartInsecureSkipsCaFile(t *testing.T) {
	archive, err := os.ReadFile("../../../testdata/rancher-cluster-templates-0.5.2.tgz")
	if err != nil {
		t.Fatalf("reading chart archive: %v", err)
	}
	index := "apiVersion: v1\nentries:\n  rancher-cluster-templates:\n  - apiVersion: v1\n    name: rancher-cluster-templates\n    version: 0.5.2\n    urls:\n    - rancher-cluster-templates-0.5.2.tgz\n"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			w.Write([]byte(index))
		case "/rancher-cluster-templates-0.5.2.tgz":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// point helm at empty temp dirs so the machine's own repositories and caches never leak into the test
	repoConfig, repoCache, contentCache := settings.RepositoryConfig, settings.RepositoryCache, settings.ContentCache
	settings.RepositoryConfig, settings.RepositoryCache, settings.ContentCache = filepath.Join(t.TempDir(), "repositories.yaml"), t.TempDir(), t.TempDir()
	t.Cleanup(func() {
		settings.RepositoryConfig, settings.RepositoryCache, settings.ContentCache = repoConfig, repoCache, contentCache
	})

	opts := &action.ChartPathOptions{
		RepoURL:               srv.URL,
		Version:               "0.5.2",
		InsecureSkipTLSVerify: true,
		CaFile:                filepath.Join(t.TempDir(), "missing.pem"),
	}
	if _, err := NewChart("rancher-cluster-templates", opts); err != nil {
		t.Fatalf("NewChart read the ca file even though insecure was set: %v", err)
	}
}
