package store

// testhelpers_test.go provides shared test helpers for cmd/hauler/cli/store tests.
//
// This file is in-package (package store) so tests can call unexported
// helpers like storeImage, storeFile, rewriteReference, etc.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	gcrv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	"helm.sh/helm/v3/pkg/action"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/store"
)

// newTestStore creates a fresh store in a temp directory. Fatal on error.
func newTestStore(t *testing.T) *store.Layout {
	t.Helper()
	s, err := store.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("newTestStore: %v", err)
	}
	return s
}

// newTestRegistry starts an in-memory OCI registry backed by httptest.
// Returns the host (host:port) and remote.Options that route requests through
// the server's plain-HTTP transport. The server is shut down via t.Cleanup.
//
// Pass the returned remoteOpts to seedImage/seedIndex and to store.AddImage
// calls so that both sides use the same plain-HTTP transport.
func newTestRegistry(t *testing.T) (host string, remoteOpts []remote.Option) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts
}

// seedImage pushes a random single-platform image to the test registry.
// repo is a bare path like "myorg/myimage"; tag is the image tag string.
// Pass the remoteOpts from newTestRegistry so writes use the correct transport.
func seedImage(t *testing.T, host, repo, tag string, opts ...remote.Option) gcrv1.Image {
	t.Helper()
	img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("seedImage random.Image: %v", err)
	}
	ref, err := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatalf("seedImage name.NewTag: %v", err)
	}
	if err := remote.Write(ref, img, opts...); err != nil {
		t.Fatalf("seedImage remote.Write: %v", err)
	}
	return img
}

// seedIndex pushes a 2-platform image index (linux/amd64 + linux/arm64) to
// the test registry. Pass the remoteOpts from newTestRegistry.
func seedIndex(t *testing.T, host, repo, tag string, opts ...remote.Option) gcrv1.ImageIndex {
	t.Helper()
	amd64Img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("seedIndex random.Image amd64: %v", err)
	}
	arm64Img, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("seedIndex random.Image arm64: %v", err)
	}
	idx := mutate.AppendManifests(
		empty.Index,
		mutate.IndexAddendum{
			Add: amd64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: arm64Img,
			Descriptor: gcrv1.Descriptor{
				MediaType: gvtypes.OCIManifestSchema1,
				Platform:  &gcrv1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)
	ref, err := name.NewTag(host+"/"+repo+":"+tag, name.Insecure)
	if err != nil {
		t.Fatalf("seedIndex name.NewTag: %v", err)
	}
	if err := remote.WriteIndex(ref, idx, opts...); err != nil {
		t.Fatalf("seedIndex remote.WriteIndex: %v", err)
	}
	return idx
}

// seedFileInHTTPServer starts an httptest server serving a single file at
// /filename with the given content. Returns the full URL. Server closed via t.Cleanup.
func seedFileInHTTPServer(t *testing.T, filename, content string) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+filename, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		io.WriteString(w, content) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filename
}

// defaultRootOpts returns a StoreRootOpts pointed at storeDir with Retries=1.
// Using Retries=1 avoids the 5-second RetriesInterval sleep in failure tests.
func defaultRootOpts(storeDir string) *flags.StoreRootOpts {
	return &flags.StoreRootOpts{
		StoreDir: storeDir,
		Retries:  1,
	}
}

// defaultCliOpts returns CliRootOpts with error-level logging and IgnoreErrors=false.
func defaultCliOpts() *flags.CliRootOpts {
	return &flags.CliRootOpts{
		IgnoreErrors: false,
		LogLevel:     "error",
	}
}

// newTestContext returns a context with a no-op zerolog logger attached so that
// log.FromContext does not emit to stdout/stderr during tests.
func newTestContext(t *testing.T) context.Context {
	t.Helper()
	zl := zerolog.New(io.Discard)
	return zl.WithContext(context.Background())
}

// newAddChartOpts builds an AddChartOpts for loading a local .tgz chart from
// repoURL (typically a testdata directory path) at the given version string.
func newAddChartOpts(repoURL, version string) *flags.AddChartOpts {
	return &flags.AddChartOpts{
		ChartOpts: &action.ChartPathOptions{
			RepoURL: repoURL,
			Version: version,
		},
	}
}

// assertArtifactInStore walks the store and fails the test if no descriptor
// has an AnnotationRefName containing refSubstring.
func assertArtifactInStore(t *testing.T, s *store.Layout, refSubstring string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("assertArtifactInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no artifact with ref containing %q found in store", refSubstring)
	}
}

// assertArtifactKindInStore walks the store and fails if no descriptor has an
// AnnotationRefName containing refSubstring AND KindAnnotationName equal to kind.
func assertArtifactKindInStore(t *testing.T, s *store.Layout, refSubstring, kind string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) &&
			desc.Annotations[consts.KindAnnotationName] == kind {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("assertArtifactKindInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no artifact with ref containing %q and kind %q found in store", refSubstring, kind)
	}
}

// countArtifactsInStore returns the number of descriptors in the store index.
func countArtifactsInStore(t *testing.T, s *store.Layout) int {
	t.Helper()
	count := 0
	if err := s.OCI.Walk(func(_ string, _ ocispec.Descriptor) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("countArtifactsInStore walk: %v", err)
	}
	return count
}

// seedCosignV2Artifacts pushes synthetic cosign v2 signature, attestation, and SBOM
// manifests at the sha256-<hex>.sig / .att / .sbom tags derived from baseImg's digest.
// Pass the remoteOpts from newLocalhostRegistry or newTestRegistry.
func seedCosignV2Artifacts(t *testing.T, host, repo string, baseImg gcrv1.Image, opts ...remote.Option) {
	t.Helper()
	hash, err := baseImg.Digest()
	if err != nil {
		t.Fatalf("seedCosignV2Artifacts: get digest: %v", err)
	}
	tagPrefix := strings.ReplaceAll(hash.String(), ":", "-")
	for _, suffix := range []string{".sig", ".att", ".sbom"} {
		img, err := random.Image(64, 1)
		if err != nil {
			t.Fatalf("seedCosignV2Artifacts: random.Image (%s): %v", suffix, err)
		}
		ref, err := name.NewTag(host+"/"+repo+":"+tagPrefix+suffix, name.Insecure)
		if err != nil {
			t.Fatalf("seedCosignV2Artifacts: NewTag (%s): %v", suffix, err)
		}
		if err := remote.Write(ref, img, opts...); err != nil {
			t.Fatalf("seedCosignV2Artifacts: Write (%s): %v", suffix, err)
		}
	}
}

// seedOCI11Referrer pushes a synthetic OCI 1.1 / cosign v3 Sigstore bundle manifest
// whose subject field points at baseImg. The in-process registry auto-registers it in
// the referrers index so remote.Referrers returns it.
// Pass the remoteOpts from newLocalhostRegistry or newTestRegistry.
func seedOCI11Referrer(t *testing.T, host, repo string, baseImg gcrv1.Image, opts ...remote.Option) {
	t.Helper()
	hash, err := baseImg.Digest()
	if err != nil {
		t.Fatalf("seedOCI11Referrer: get digest: %v", err)
	}
	rawManifest, err := baseImg.RawManifest()
	if err != nil {
		t.Fatalf("seedOCI11Referrer: raw manifest: %v", err)
	}
	mt, err := baseImg.MediaType()
	if err != nil {
		t.Fatalf("seedOCI11Referrer: media type: %v", err)
	}
	baseDesc := gcrv1.Descriptor{
		MediaType: mt,
		Digest:    hash,
		Size:      int64(len(rawManifest)),
	}

	bundleJSON := []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}`)
	bundleLayer := static.NewLayer(bundleJSON, gvtypes.MediaType(consts.SigstoreBundleMediaType))
	referrerImg, err := mutate.AppendLayers(empty.Image, bundleLayer)
	if err != nil {
		t.Fatalf("seedOCI11Referrer: AppendLayers: %v", err)
	}
	referrerImg = mutate.MediaType(referrerImg, gvtypes.OCIManifestSchema1)
	referrerImg = mutate.ConfigMediaType(referrerImg, gvtypes.MediaType(consts.OCIEmptyConfigMediaType))
	referrerImg = mutate.Subject(referrerImg, baseDesc).(gcrv1.Image)

	referrerTag, err := name.NewTag(host+"/"+repo+":bundle-referrer", name.Insecure)
	if err != nil {
		t.Fatalf("seedOCI11Referrer: NewTag: %v", err)
	}
	if err := remote.Write(referrerTag, referrerImg, opts...); err != nil {
		t.Fatalf("seedOCI11Referrer: Write: %v", err)
	}
}

// assertReferrerInStore walks the store and fails if no descriptor has a kind
// annotation with the KindAnnotationReferrers prefix and a ref containing refSubstring.
func assertReferrerInStore(t *testing.T, s *store.Layout, refSubstring string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) &&
			strings.HasPrefix(desc.Annotations[consts.KindAnnotationName], consts.KindAnnotationReferrers) {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("assertReferrerInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no OCI referrer with ref containing %q found in store", refSubstring)
	}
}
