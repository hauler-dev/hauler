package store

// testhelpers_test.go provides shared test helpers for cmd/hauler/cli/store tests.
//
// This file is in-package (package store) so tests can call unexported
// helpers like storeImage, storeFile, rewriteReference, etc.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	golog "log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	digest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"
	ociempty "github.com/sigstore/cosign/v3/pkg/oci/empty"
	ocimutate "github.com/sigstore/cosign/v3/pkg/oci/mutate"
	ocistatic "github.com/sigstore/cosign/v3/pkg/oci/static"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/sigstore/sigstore/pkg/signature/payload"
	"helm.sh/helm/v4/pkg/action"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// newTestKeyPair generates a fresh ECDSA P-256 key pair and writes the public
// half in the PEM form cosign's --key expects, returning the private key and
// that path. Generated rather than vendored so the tests never depend on a
// fixture whose algorithm cosign might later stop accepting.
func newTestKeyPair(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshaling public key: %v", err)
	}

	path := filepath.Join(t.TempDir(), "cosign.pub")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing public key: %v", err)
	}
	return priv, path
}

// writeTestPubKey returns the path to a public key with no signed image behind
// it -- for tests that only need verification to fail closed.
func writeTestPubKey(t *testing.T) string {
	t.Helper()
	_, path := newTestKeyPair(t)
	return path
}

// seedSignedImage pushes a random image and a genuine cosign v2 signature for
// it, returning the image and the path to the public key that verifies it.
//
// The signature is built the way cosign itself builds one -- a simplesigning
// payload naming the image's digest, signed with ECDSA-P256, carried as the
// dev.cosignproject.cosign/signature annotation on a layer of the
// sha256-<hex>.sig manifest -- so cosign.Verifier's library path accepts it
// with IgnoreTlog set. seedCosignV2Artifacts pushes the same tags with random
// content and is its fail-closed counterpart.
func seedSignedImage(t *testing.T, host, repo, tag string, opts ...remote.Option) (gcrv1.Image, string) {
	t.Helper()

	img := seedImage(t, host, repo, tag, opts...)
	hash, err := img.Digest()
	if err != nil {
		t.Fatalf("seedSignedImage digest: %v", err)
	}

	priv, keyPath := newTestKeyPair(t)
	sv, err := signature.LoadECDSASignerVerifier(priv, crypto.SHA256)
	if err != nil {
		t.Fatalf("seedSignedImage LoadECDSASignerVerifier: %v", err)
	}

	digestRef, err := name.NewDigest(host+"/"+repo+"@"+hash.String(), name.Insecure)
	if err != nil {
		t.Fatalf("seedSignedImage NewDigest: %v", err)
	}
	// The payload binds the signature to this exact digest; cosign rejects a
	// signature whose payload names a different one, which is what makes the
	// pinning assertions meaningful.
	payloadBytes, err := (&payload.Cosign{Image: digestRef}).MarshalJSON()
	if err != nil {
		t.Fatalf("seedSignedImage marshaling payload: %v", err)
	}
	rawSig, err := sv.SignMessage(bytes.NewReader(payloadBytes))
	if err != nil {
		t.Fatalf("seedSignedImage SignMessage: %v", err)
	}

	ociSig, err := ocistatic.NewSignature(payloadBytes, base64.StdEncoding.EncodeToString(rawSig))
	if err != nil {
		t.Fatalf("seedSignedImage ocistatic.NewSignature: %v", err)
	}
	sigs, err := ocimutate.AppendSignatures(ociempty.Signatures(), false, ociSig)
	if err != nil {
		t.Fatalf("seedSignedImage AppendSignatures: %v", err)
	}

	sigRef, err := name.NewTag(host+"/"+repo+":"+strings.ReplaceAll(hash.String(), ":", "-")+".sig", name.Insecure)
	if err != nil {
		t.Fatalf("seedSignedImage NewTag (sig): %v", err)
	}
	if err := remote.Write(sigRef, sigs, opts...); err != nil {
		t.Fatalf("seedSignedImage writing signature: %v", err)
	}
	return img, keyPath
}

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

// recordingHandler records the path of every request it serves before
// delegating. Concurrent handlers append to the same slice, so every method
// takes mu.
type recordingHandler struct {
	next  http.Handler
	mu    sync.Mutex
	paths []string
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.paths = append(h.paths, r.Method+" "+r.URL.Path)
	h.mu.Unlock()
	h.next.ServeHTTP(w, r)
}

func (h *recordingHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.paths = nil
}

func (h *recordingHandler) countContaining(sub string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, p := range h.paths {
		if strings.Contains(p, sub) {
			n++
		}
	}
	return n
}

func (h *recordingHandler) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.paths...)
}

// newRecordingRegistry mirrors newTestRegistry but records request paths, so a
// test can assert how a tag was reached and not just what was stored.
func newRecordingRegistry(t *testing.T) (string, []remote.Option, *recordingHandler) {
	t.Helper()
	rec := &recordingHandler{next: registry.New(registry.Logger(golog.New(io.Discard, "", 0)))}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), []remote.Option{remote.WithTransport(srv.Client().Transport)}, rec
}

// seedImage pushes a random single-platform image to the test registry.
// repo is a bare path like "myorg/myimage"... tag is the image tag string.
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

// defaultCliOpts returns CliRootOpts with log level error, audit level none, and ignore errors false.
// Audit is disabled here rather than pointed at a temp HaulerDir so tests don't write to the
// developer/CI user's real $HOME/.hauler/audit.log.
func defaultCliOpts() *flags.CliRootOpts {
	return &flags.CliRootOpts{
		IgnoreErrors: false,
		LogLevel:     "error",
		AuditLevel:   "none",
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

// assertArtifactNotInStore is assertArtifactInStore's opposite: fails if any
// descriptor's AnnotationRefName contains refSubstring.
func assertArtifactNotInStore(t *testing.T, s *store.Layout, refSubstring string) {
	t.Helper()
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) {
			t.Errorf("expected no artifact with ref containing %q, found one in store", refSubstring)
		}
		return nil
	}); err != nil {
		t.Fatalf("assertArtifactNotInStore walk: %v", err)
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

// storedDigest returns the digest of the first indexed descriptor whose ref
// annotation contains refSubstring, or "" if there is none.
func storedDigest(t *testing.T, s *store.Layout, refSubstring string) string {
	t.Helper()
	found := ""
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if found == "" && strings.Contains(desc.Annotations[ocispec.AnnotationRefName], refSubstring) {
			found = desc.Digest.String()
		}
		return nil
	}); err != nil {
		t.Fatalf("storedDigest walk: %v", err)
	}
	return found
}

// lastAuditEntryFlags reads <haulerDir>/audit.log (see pkg/audit.Append /
// resolveDir) and returns the "flags" object of its last JSON line -- only
// populated at audit level "verbose", since audit.Append omits Flags
// otherwise. Fails the test if the file is missing, empty, or malformed.
func lastAuditEntryFlags(t *testing.T, haulerDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(haulerDir, "audit.log"))
	if err != nil {
		t.Fatalf("reading audit.log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatalf("audit.log has no entries")
	}
	var entry struct {
		Flags map[string]any `json:"flags"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("unmarshaling last audit.log line: %v\nline: %s", err, lines[len(lines)-1])
	}
	return entry.Flags
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

// seedStoreDescriptor injects a descriptor with the given annotations directly
// into the store index without requiring a real registry or blob. This is used
// to pre-populate the store for rewriteReference unit tests.
func seedStoreDescriptor(t *testing.T, s *store.Layout, annotations map[string]string) {
	t.Helper()
	desc := ocispec.Descriptor{
		MediaType:   ocispec.MediaTypeImageManifest,
		Digest:      digest.Digest("sha256:" + strings.Repeat("a", 64)),
		Size:        1,
		Annotations: annotations,
	}
	if err := s.OCI.AddIndex(desc); err != nil {
		t.Fatalf("seedStoreDescriptor: %v", err)
	}
}

// assertAnnotationsInStore walks the store and fails if no descriptor has both
// AnnotationRefName == refName AND ContainerdImageNameKey == containerdName.
func assertAnnotationsInStore(t *testing.T, s *store.Layout, refName, containerdName string) {
	t.Helper()
	found := false
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		if desc.Annotations[ocispec.AnnotationRefName] == refName &&
			desc.Annotations[consts.ContainerdImageNameKey] == containerdName {
			found = true
		}
		return nil
	}); err != nil {
		t.Fatalf("assertAnnotationsInStore walk: %v", err)
	}
	if !found {
		t.Errorf("no artifact with AnnotationRefName=%q and ContainerdImageNameKey=%q found in store", refName, containerdName)
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
