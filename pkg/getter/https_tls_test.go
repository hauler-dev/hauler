package getter_test

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/getter"
)

// A bad ca file must fail a remote fetch before any request goes out, insecure must win without reading it, and a good ca file must be trusted.
func TestHttp_Open_CaFile(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL + "/install.sh")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatalf("writing ca file: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.pem")

	_, err = getter.NewHttp(false, missing).Open(context.Background(), u)
	if err == nil || !strings.Contains(err.Error(), "reading CA file") {
		t.Fatalf("Open with a missing ca file returned %v, want a ca file error", err)
	}
	if hits.Load() != 0 {
		t.Fatal("a request went out despite the missing ca file")
	}

	for name, h := range map[string]*getter.Http{"insecure over a missing ca file": getter.NewHttp(true, missing), "valid ca file": getter.NewHttp(false, caFile)} {
		rc, err := h.Open(context.Background(), u)
		if err != nil {
			t.Fatalf("%s: Open: %v", name, err)
		}
		rc.Close()
	}
}

// A bad ca file only matters for remote fetches, so adding a local file must still work.
func TestClient_LocalFileIgnoresBadCaFile(t *testing.T) {
	local := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(local, []byte("local"), 0o600); err != nil {
		t.Fatalf("writing local file: %v", err)
	}
	c := getter.NewClient(getter.ClientOptions{CAFile: filepath.Join(t.TempDir(), "missing.pem")})
	if _, err := c.LayerFrom(context.Background(), local); err != nil {
		t.Fatalf("local file failed on an unrelated ca file: %v", err)
	}
}
