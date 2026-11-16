package store

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	goname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// The digest pin that runs before signature verification must honor the tls flags like the image pull does.
func TestPinDigestHonorsTLSFlags(t *testing.T) {
	srv := httptest.NewTLSServer(registry.New())
	defer srv.Close()

	ref, err := goname.ParseReference(strings.TrimPrefix(srv.URL, "https://") + "/test/pin:v1")
	if err != nil {
		t.Fatalf("parsing reference: %v", err)
	}
	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}
	insecure := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} // #nosec G402
	if err := remote.Write(ref, img, remote.WithTransport(insecure)); err != nil {
		t.Fatalf("seeding image: %v", err)
	}
	want, err := img.Digest()
	if err != nil {
		t.Fatalf("image digest: %v", err)
	}

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatalf("writing ca file: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.pem")

	ctx := newTestContext(t)
	rso, ro := defaultRootOpts(t.TempDir()), defaultCliOpts()

	if _, err := pinDigest(ctx, ref, rso, ro, false, ""); err == nil {
		t.Fatal("control failed: the test registry was trusted without its ca")
	}
	for name, tc := range map[string]struct {
		insecure bool
		caFile   string
	}{
		"insecure only":                 {insecure: true},
		"ca file only":                  {caFile: caFile},
		"insecure and ca file":          {insecure: true, caFile: caFile},
		"insecure wins over missing ca": {insecure: true, caFile: missing},
	} {
		got, err := pinDigest(ctx, ref, rso, ro, tc.insecure, tc.caFile)
		if err != nil {
			t.Fatalf("%s: pinDigest: %v", name, err)
		}
		if got != want.String() {
			t.Fatalf("%s: pinned %s, want %s", name, got, want)
		}
	}
}
