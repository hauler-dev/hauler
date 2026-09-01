package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ccontent "github.com/containerd/containerd/v2/core/content"
	goname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/reference"
	"hauler.dev/go/hauler/v2/pkg/store"
)

var (
	ctx  context.Context
	root string
)

func TestLayout_AddArtifact(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	type args struct {
		ref string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "",
			args: args{
				ref: "hello/world:v1",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := store.NewLayout(root)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOCI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			moci := genArtifact(t, tt.args.ref)

			got, err := s.AddArtifact(ctx, moci, tt.args.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddArtifact() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			_ = got

			_, err = s.AddArtifact(ctx, moci, tt.args.ref)
			if err != nil {
				t.Errorf("AddArtifact() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func setup(t *testing.T) func() error {
	tempDir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}
	root = tempDir

	ctx = context.Background()

	return func() error {
		os.RemoveAll(tempDir)
		return nil
	}
}

type mockArtifact struct {
	v1.Image
}

func (m mockArtifact) MediaType() string {
	mt, err := m.Image.MediaType()
	if err != nil {
		return ""
	}
	return string(mt)
}

func (m mockArtifact) RawConfig() ([]byte, error) {
	return m.RawConfigFile()
}

func genArtifact(t *testing.T, ref string) artifacts.OCI {
	img, err := random.Image(1024, 3)
	if err != nil {
		t.Fatal(err)
	}

	return &mockArtifact{
		img,
	}
}

// Mock fetcher/pusher for testing
type mockFetcher struct {
	blobs map[digest.Digest][]byte
}

func newMockFetcher() *mockFetcher {
	return &mockFetcher{
		blobs: make(map[digest.Digest][]byte),
	}
}

func (m *mockFetcher) addBlob(data []byte) ocispec.Descriptor {
	dgst := digest.FromBytes(data)
	m.blobs[dgst] = data
	return ocispec.Descriptor{
		MediaType: "application/octet-stream",
		Digest:    dgst,
		Size:      int64(len(data)),
	}
}

func (m *mockFetcher) Fetch(ctx context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	data, ok := m.blobs[desc.Digest]
	if !ok {
		return nil, fmt.Errorf("blob not found: %s", desc.Digest)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type mockPusher struct {
	blobs map[digest.Digest][]byte
}

func newMockPusher() *mockPusher {
	return &mockPusher{
		blobs: make(map[digest.Digest][]byte),
	}
}

func (m *mockPusher) Push(ctx context.Context, desc ocispec.Descriptor) (ccontent.Writer, error) {
	return &mockWriter{
		pusher: m,
		desc:   desc,
		buf:    &bytes.Buffer{},
	}, nil
}

type mockWriter struct {
	pusher *mockPusher
	desc   ocispec.Descriptor
	buf    *bytes.Buffer
	closed bool
}

func (m *mockWriter) Write(p []byte) (int, error) {
	if m.closed {
		return 0, fmt.Errorf("writer closed")
	}
	return m.buf.Write(p)
}

func (m *mockWriter) Close() error {
	m.closed = true
	return nil
}

func (m *mockWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...ccontent.Opt) error {
	data := m.buf.Bytes()
	if int64(len(data)) != size {
		return fmt.Errorf("size mismatch: expected %d, got %d", size, len(data))
	}
	dgst := digest.FromBytes(data)
	if expected != "" && dgst != expected {
		return fmt.Errorf("digest mismatch: expected %s, got %s", expected, dgst)
	}
	m.pusher.blobs[dgst] = data
	return nil
}

func (m *mockWriter) Digest() digest.Digest {
	return digest.FromBytes(m.buf.Bytes())
}

func (m *mockWriter) Status() (ccontent.Status, error) {
	return ccontent.Status{}, nil
}

func (m *mockWriter) Truncate(size int64) error {
	return fmt.Errorf("truncate not supported")
}

// blobPath returns the expected filesystem path for a blob in an OCI layout store.
func blobPath(root string, d digest.Digest) string {
	return filepath.Join(root, "blobs", d.Algorithm().String(), d.Encoded())
}

// findRefKey walks the store's index and returns the nameMap key for the first
// descriptor whose AnnotationRefName matches ref.
func findRefKey(t *testing.T, s *store.Layout, ref string) string {
	t.Helper()
	var key string
	_ = s.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		if desc.Annotations[ocispec.AnnotationRefName] == ref && key == "" {
			key = reference
		}
		return nil
	})
	if key == "" {
		t.Fatalf("reference %q not found in store", ref)
	}
	return key
}

// findRefKeyByKind walks the store's index and returns the nameMap key for the
// descriptor whose AnnotationRefName matches ref and whose kind annotation matches kind.
func findRefKeyByKind(t *testing.T, s *store.Layout, ref, kind string) string {
	t.Helper()
	var key string
	_ = s.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		if desc.Annotations[ocispec.AnnotationRefName] == ref &&
			desc.Annotations[consts.KindAnnotationName] == kind {
			key = reference
		}
		return nil
	})
	if key == "" {
		t.Fatalf("reference %q with kind %q not found in store", ref, kind)
	}
	return key
}

// readManifestBlob reads and parses an OCI manifest from the store's blob directory.
func readManifestBlob(t *testing.T, root string, d digest.Digest) ocispec.Manifest {
	t.Helper()
	data, err := os.ReadFile(blobPath(root, d))
	if err != nil {
		t.Fatalf("read manifest blob %s: %v", d, err)
	}
	var m ocispec.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	return m
}

// TestCopyDescriptor verifies that copyDescriptor (exercised via Copy) transfers
// each individual blob — config and every layer — into the destination store's blob
// directory, and that a second Copy of the same content succeeds gracefully when
// blobs are already present (AlreadyExists path).
func TestCopyDescriptor(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	srcRoot := t.TempDir()
	src, err := store.NewLayout(srcRoot)
	if err != nil {
		t.Fatal(err)
	}

	ref := "test/blob:v1"
	// genArtifact creates random.Image(1024, 3): 1 config blob + 3 layer blobs.
	manifestDesc, err := src.AddArtifact(ctx, genArtifact(t, ref), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.OCI.SaveIndex(); err != nil {
		t.Fatal(err)
	}

	refKey := findRefKey(t, src, ref)
	manifest := readManifestBlob(t, srcRoot, manifestDesc.Digest)

	dstRoot := t.TempDir()
	dst, err := store.NewLayout(dstRoot)
	if err != nil {
		t.Fatal(err)
	}

	// First copy: should transfer all individual blobs via copyDescriptor.
	gotDesc, err := src.Copy(ctx, refKey, dst.OCI, "test/blob:dst")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	if gotDesc.Digest != manifestDesc.Digest {
		t.Errorf("returned descriptor digest mismatch: got %s, want %s", gotDesc.Digest, manifestDesc.Digest)
	}

	// Verify the config blob is present in the destination.
	if _, err := os.Stat(blobPath(dstRoot, manifest.Config.Digest)); err != nil {
		t.Errorf("config blob missing in dest: %v", err)
	}

	// Verify every layer blob is present in the destination.
	for i, layer := range manifest.Layers {
		if _, err := os.Stat(blobPath(dstRoot, layer.Digest)); err != nil {
			t.Errorf("layer[%d] blob missing in dest: %v", i, err)
		}
	}

	// Verify the manifest blob itself was pushed.
	if _, err := os.Stat(blobPath(dstRoot, manifestDesc.Digest)); err != nil {
		t.Errorf("manifest blob missing in dest: %v", err)
	}

	// Second copy: blobs already exist — AlreadyExists must be handled without error.
	gotDesc2, err := src.Copy(ctx, refKey, dst.OCI, "test/blob:dst2")
	if err != nil {
		t.Fatalf("second Copy failed (AlreadyExists should be a no-op): %v", err)
	}
	if gotDesc2.Digest != manifestDesc.Digest {
		t.Errorf("second Copy digest mismatch: got %s, want %s", gotDesc2.Digest, manifestDesc.Digest)
	}
}

// TestCopyDescriptorGraph_Manifest verifies that copyDescriptorGraph reconstructs a
// complete manifest in the destination (config digest and each layer digest match the
// source), and returns an error when a required blob is absent from the source.
func TestCopyDescriptorGraph_Manifest(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	srcRoot := t.TempDir()
	src, err := store.NewLayout(srcRoot)
	if err != nil {
		t.Fatal(err)
	}

	ref := "test/manifest:v1"
	manifestDesc, err := src.AddArtifact(ctx, genArtifact(t, ref), ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.OCI.SaveIndex(); err != nil {
		t.Fatal(err)
	}

	refKey := findRefKey(t, src, ref)
	srcManifest := readManifestBlob(t, srcRoot, manifestDesc.Digest)

	// --- Happy path: all blobs present, manifest structure preserved ---
	dstRoot := t.TempDir()
	dst, err := store.NewLayout(dstRoot)
	if err != nil {
		t.Fatal(err)
	}

	gotDesc, err := src.Copy(ctx, refKey, dst.OCI, "test/manifest:dst")
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	// Parse the manifest from the destination and compare structure with source.
	dstManifest := readManifestBlob(t, dstRoot, gotDesc.Digest)
	if dstManifest.Config.Digest != srcManifest.Config.Digest {
		t.Errorf("config digest mismatch: got %s, want %s",
			dstManifest.Config.Digest, srcManifest.Config.Digest)
	}
	if len(dstManifest.Layers) != len(srcManifest.Layers) {
		t.Fatalf("layer count mismatch: dst=%d src=%d",
			len(dstManifest.Layers), len(srcManifest.Layers))
	}
	for i, l := range srcManifest.Layers {
		if dstManifest.Layers[i].Digest != l.Digest {
			t.Errorf("layer[%d] digest mismatch: got %s, want %s",
				i, dstManifest.Layers[i].Digest, l.Digest)
		}
	}

	// --- Error path: delete a layer blob from source, expect Copy to fail ---
	if len(srcManifest.Layers) == 0 {
		t.Skip("artifact has no layers... skipping missing blob error path")
	}
	if err := os.Remove(blobPath(srcRoot, srcManifest.Layers[0].Digest)); err != nil {
		t.Fatalf("could not remove layer blob to simulate corruption: %v", err)
	}

	dst2Root := t.TempDir()
	dst2, err := store.NewLayout(dst2Root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = src.Copy(ctx, refKey, dst2.OCI, "test/manifest:missing-blob")
	if err == nil {
		t.Error("expected Copy to fail when a source layer blob is missing, but it succeeded")
	}
}

// TestCopyDescriptorGraph_Index verifies that copyDescriptorGraph handles an OCI
// image index (multi-platform) by recursively copying all child manifests and their
// blobs into the destination store, and that the index blob itself is present.
func TestCopyDescriptorGraph_Index(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	// Start an in-process OCI registry.
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	remoteOpts := []remote.Option{remote.WithTransport(srv.Client().Transport)}

	// Build a 2-platform image index.
	img1, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random image (amd64): %v", err)
	}
	img2, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random image (arm64): %v", err)
	}
	idx := mutate.AppendManifests(
		empty.Index,
		mutate.IndexAddendum{
			Add: img1,
			Descriptor: v1.Descriptor{
				MediaType: types.OCIManifestSchema1,
				Platform:  &v1.Platform{OS: "linux", Architecture: "amd64"},
			},
		},
		mutate.IndexAddendum{
			Add: img2,
			Descriptor: v1.Descriptor{
				MediaType: types.OCIManifestSchema1,
				Platform:  &v1.Platform{OS: "linux", Architecture: "arm64"},
			},
		},
	)

	idxTag, err := goname.NewTag(host+"/test/multiarch:v1", goname.Insecure)
	if err != nil {
		t.Fatalf("new tag: %v", err)
	}
	if err := remote.WriteIndex(idxTag, idx, remoteOpts...); err != nil {
		t.Fatalf("push index: %v", err)
	}

	// Pull the index into a hauler store via AddImage.
	srcRoot := t.TempDir()
	src, err := store.NewLayout(srcRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.AddImage(ctx, idxTag.Name(), "", false, "", false, "", remoteOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if err := src.OCI.SaveIndex(); err != nil {
		t.Fatal(err)
	}

	// Locate the index descriptor (kind=imageIndex) in the source store.
	refKey := findRefKeyByKind(t, src, "test/multiarch:v1", consts.KindAnnotationIndex)

	// Copy the entire index graph to a fresh destination store.
	dstRoot := t.TempDir()
	dst, err := store.NewLayout(dstRoot)
	if err != nil {
		t.Fatal(err)
	}
	gotDesc, err := src.Copy(ctx, refKey, dst.OCI, "test/multiarch:copied")
	if err != nil {
		t.Fatalf("Copy of image index failed: %v", err)
	}

	// The index blob itself must be present in the destination.
	if _, err := os.Stat(blobPath(dstRoot, gotDesc.Digest)); err != nil {
		t.Errorf("index manifest blob missing in dest: %v", err)
	}

	// Parse the index from the source and verify every child manifest blob landed
	// in the destination (exercising recursive copyDescriptorGraph for each child).
	var ociIdx ocispec.Index
	if err := json.Unmarshal(mustReadFile(t, blobPath(srcRoot, gotDesc.Digest)), &ociIdx); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	if len(ociIdx.Manifests) < 2 {
		t.Fatalf("expected ≥2 child manifests in index, got %d", len(ociIdx.Manifests))
	}
	for i, child := range ociIdx.Manifests {
		if _, err := os.Stat(blobPath(dstRoot, child.Digest)); err != nil {
			t.Errorf("child manifest[%d] (platform=%v) blob missing in dest: %v",
				i, child.Platform, err)
		}
	}
}

// mustReadFile reads a file and fails the test on error.
func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestCopy_Integration tests the full Copy workflow including copyDescriptorGraph
func TestCopy_Integration(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	// Create source store
	sourceRoot, err := os.MkdirTemp("", "hauler-source")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sourceRoot)

	sourceStore, err := store.NewLayout(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Add an artifact to source
	ref := "test/image:v1"
	artifact := genArtifact(t, ref)
	_, err = sourceStore.AddArtifact(ctx, artifact, ref)
	if err != nil {
		t.Fatal(err)
	}

	// Save the index to persist the reference
	if err := sourceStore.OCI.SaveIndex(); err != nil {
		t.Fatalf("Failed to save index: %v", err)
	}

	// Find the actual reference key in the nameMap (includes kind suffix)
	var sourceRefKey string
	err = sourceStore.OCI.Walk(func(reference string, desc ocispec.Descriptor) error {
		if desc.Annotations[ocispec.AnnotationRefName] == ref {
			sourceRefKey = reference
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk source store: %v", err)
	}
	if sourceRefKey == "" {
		t.Fatal("Failed to find reference in source store")
	}

	// Create destination store
	destRoot, err := os.MkdirTemp("", "hauler-dest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(destRoot)

	destStore, err := store.NewLayout(destRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Copy from source to destination
	destRef := "test/image:copied"
	desc, err := sourceStore.Copy(ctx, sourceRefKey, destStore.OCI, destRef)
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	// Copy doesn't automatically add to destination index for generic targets
	// For OCI stores, we need to add the descriptor manually with the reference
	desc.Annotations = map[string]string{
		ocispec.AnnotationRefName: destRef,
		consts.KindAnnotationName: consts.KindAnnotationImage,
	}
	if err := destStore.OCI.AddIndex(desc); err != nil {
		t.Fatalf("Failed to add descriptor to destination index: %v", err)
	}

	// Verify the descriptor was copied
	if desc.Digest == "" {
		t.Error("Expected non-empty digest")
	}

	// Find the copied reference in destination
	var foundInDest bool
	var destDesc ocispec.Descriptor
	err = destStore.OCI.Walk(func(reference string, d ocispec.Descriptor) error {
		if d.Digest == desc.Digest {
			foundInDest = true
			destDesc = d
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk destination store: %v", err)
	}

	if !foundInDest {
		t.Error("Copied descriptor not found in destination store")
	}

	if destDesc.Digest != desc.Digest {
		t.Errorf("Digest mismatch: got %s, want %s", destDesc.Digest, desc.Digest)
	}
}

// TestCopy_ErrorHandling tests error cases
func TestCopy_ErrorHandling(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	sourceRoot, err := os.MkdirTemp("", "hauler-source")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sourceRoot)

	sourceStore, err := store.NewLayout(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}

	destRoot, err := os.MkdirTemp("", "hauler-dest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(destRoot)

	destStore, err := store.NewLayout(destRoot)
	if err != nil {
		t.Fatal(err)
	}

	// Test copying non-existent reference
	_, err = sourceStore.Copy(ctx, "nonexistent:tag", destStore.OCI, "dest:tag")
	if err == nil {
		t.Error("Expected error when copying non-existent reference")
	}
}

// TestCopy_DockerFormats tests copying Docker manifest formats
func TestCopy_DockerFormats(t *testing.T) {
	// This test verifies that Docker format media types are recognized
	// The actual copying is tested in the integration test
	if consts.DockerManifestSchema2 == "" {
		t.Error("DockerManifestSchema2 constant should not be empty")
	}
	t.Skip("Docker format copying is tested via integration tests")
}

// TestCopy_MultiPlatform tests copying multi-platform images with manifest lists
func TestCopy_MultiPlatform(t *testing.T) {
	teardown := setup(t)
	defer teardown()

	sourceRoot, err := os.MkdirTemp("", "hauler-source")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sourceRoot)

	// This test would require creating a multi-platform image
	// which is more complex - marking as future enhancement
	t.Skip("Multi-platform image test requires additional setup")
}

// TestAddImage_OCI11Referrers verifies that AddImage captures OCI 1.1 referrers
// (cosign v3 new-bundle-format) stored via the subject field rather than the legacy
// sha256-<hex>.sig/.att/.sbom tag convention.
//
// The test:
//  1. Starts an in-process OCI 1.1–capable registry (go-containerregistry/pkg/registry)
//  2. Pushes a random base image to it
//  3. Builds a synthetic cosign v3-style Sigstore bundle referrer manifest (with a
//     "subject" field pointing at the base image) and pushes it so the registry
//     registers it in the referrers index automatically
//  4. Calls store.AddImage and then walks the OCI layout to confirm that a
//     KindAnnotationReferrers-prefixed entry was saved
func TestAddImage_OCI11Referrers(t *testing.T) {
	// 1. Start an in-process OCI 1.1 registry.
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	remoteOpts := []remote.Option{
		remote.WithTransport(srv.Client().Transport),
	}

	// 2. Push a random base image.
	baseTag, err := goname.NewTag(host+"/test/image:v1", goname.Insecure)
	if err != nil {
		t.Fatalf("new tag: %v", err)
	}
	baseImg, err := random.Image(512, 2)
	if err != nil {
		t.Fatalf("random image: %v", err)
	}
	if err := remote.Write(baseTag, baseImg, remoteOpts...); err != nil {
		t.Fatalf("push base image: %v", err)
	}

	// Build the v1.Descriptor for the base image so we can set it as the referrer subject.
	baseHash, err := baseImg.Digest()
	if err != nil {
		t.Fatalf("base image digest: %v", err)
	}
	baseRawManifest, err := baseImg.RawManifest()
	if err != nil {
		t.Fatalf("base image raw manifest: %v", err)
	}
	baseMT, err := baseImg.MediaType()
	if err != nil {
		t.Fatalf("base image media type: %v", err)
	}
	baseDesc := v1.Descriptor{
		MediaType: baseMT,
		Digest:    baseHash,
		Size:      int64(len(baseRawManifest)),
	}

	// 3. Build a synthetic cosign v3 Sigstore bundle referrer.
	//
	// Real cosign new-bundle-format: artifactType=application/vnd.dev.sigstore.bundle.v0.3+json,
	// config.mediaType=application/vnd.oci.empty.v1+json, single layer containing the bundle JSON,
	// and a "subject" field pointing at the base image digest.
	bundleJSON := []byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json",` +
		`"verificationMaterial":{},"messageSignature":{"messageDigest":` +
		`{"algorithm":"SHA2_256","digest":"AAAA"},"signature":"AAAA"}}`)
	bundleLayer := static.NewLayer(bundleJSON, types.MediaType(consts.SigstoreBundleMediaType))

	referrerImg, err := mutate.AppendLayers(empty.Image, bundleLayer)
	if err != nil {
		t.Fatalf("append bundle layer: %v", err)
	}
	referrerImg = mutate.MediaType(referrerImg, types.OCIManifestSchema1)
	referrerImg = mutate.ConfigMediaType(referrerImg, types.MediaType(consts.OCIEmptyConfigMediaType))
	referrerImg = mutate.Subject(referrerImg, baseDesc).(v1.Image)

	// Push the referrer under an arbitrary tag... the in-process registry auto-wires the
	// subject field and makes the manifest discoverable via GET /v2/.../referrers/<digest>.
	referrerTag, err := goname.NewTag(host+"/test/image:bundle-referrer", goname.Insecure)
	if err != nil {
		t.Fatalf("referrer tag: %v", err)
	}
	if err := remote.Write(referrerTag, referrerImg, remoteOpts...); err != nil {
		t.Fatalf("push referrer: %v", err)
	}

	// 4. Let hauler add the base image (which should also fetch its OCI referrers).
	storeRoot := t.TempDir()
	s, err := store.NewLayout(storeRoot)
	if err != nil {
		t.Fatalf("new layout: %v", err)
	}
	if _, err := s.AddImage(context.Background(), baseTag.Name(), "", false, "", false, "", remoteOpts...); err != nil {
		t.Fatalf("AddImage: %v", err)
	}

	// 5. Walk the store and verify that at least one referrer entry was captured.
	var referrerCount int
	if err := s.Walk(func(_ string, desc ocispec.Descriptor) error {
		if strings.HasPrefix(desc.Annotations[consts.KindAnnotationName], consts.KindAnnotationReferrers) {
			referrerCount++
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if referrerCount == 0 {
		t.Fatal("expected at least one OCI referrer entry in the store, got none")
	}
	t.Logf("captured %d OCI referrer(s) for %s", referrerCount, baseTag.Name())
}

// newTestRegistry starts an in-process registry and returns its host and the
// remote.Option needed to talk to it over plain HTTP.
func newTestRegistry(t *testing.T) (string, []remote.Option) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	return host, []remote.Option{remote.WithTransport(srv.Client().Transport)}
}

// newTestStore creates a fresh OCI layout store rooted in a temp directory.
func newTestStore(t *testing.T) *store.Layout {
	t.Helper()
	s, err := store.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("new layout: %v", err)
	}
	return s
}

// seedImage pushes a random image to host/repo:tag and returns it, so a test
// can later assert on the exact bytes/digest that were pushed.
func seedImage(t *testing.T, host, repo, tag string, opts ...remote.Option) v1.Image {
	t.Helper()
	img, err := random.Image(1024, 3)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	ref, err := goname.NewTag(host+"/"+repo+":"+tag, goname.Insecure)
	if err != nil {
		t.Fatalf("new tag: %v", err)
	}
	if err := remote.Write(ref, img, opts...); err != nil {
		t.Fatalf("remote.Write: %v", err)
	}
	return img
}

// TestAddImagePinnedDigestIgnoresMovedTag proves the TOCTOU fix: once a caller
// pins the digest it verified, a tag that moves to different content between
// verification and the fetch cannot substitute its bytes into the store.
func TestAddImagePinnedDigestIgnoresMovedTag(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	original := seedImage(t, host, "test/pinned", "v1", remoteOpts...)
	originalDigest, err := original.Digest()
	if err != nil {
		t.Fatalf("original digest: %v", err)
	}

	// Move the tag to different content, exactly as a mutable tag could be
	// re-pushed between verification and the pull.
	replacement, err := random.Image(1024, 3)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	ref, err := reference.ParseReference(host + "/test/pinned:v1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := remote.Write(ref, replacement, remoteOpts...); err != nil {
		t.Fatalf("remote.Write replacement: %v", err)
	}

	s := newTestStore(t)
	got, err := s.AddImage(context.Background(), host+"/test/pinned:v1", "", true,
		originalDigest.String(), false, "", remoteOpts...)
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if got != originalDigest.String() {
		t.Fatalf("stored digest = %s, want the pinned %s (the moved tag won)", got, originalDigest.String())
	}
}

// TestAddImageEmptyPinResolvesTag confirms the unpinned path is untouched:
// an empty pinnedDigest still resolves the tag normally.
func TestAddImageEmptyPinResolvesTag(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	img := seedImage(t, host, "test/unpinned", "v1", remoteOpts...)
	want, err := img.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}

	s := newTestStore(t)
	got, err := s.AddImage(context.Background(), host+"/test/unpinned:v1", "", true, "", false, "", remoteOpts...)
	if err != nil {
		t.Fatalf("AddImage: %v", err)
	}
	if got != want.String() {
		t.Fatalf("stored digest = %s, want %s", got, want.String())
	}
}

// TestAddImage_OriginalRefAnnotation verifies that AddImage captures the original,
// fully pullable containerd-style reference (registry/repo:tag) under
// consts.OriginalRefAnnotation, for both single-platform images (writeImage) and
// multi-platform indices (writeIndex), so that provenance survives even if the
// ref/containerd-name annotations are later overwritten by a rewrite.
func TestAddImage_OriginalRefAnnotation(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")

	remoteOpts := []remote.Option{
		remote.WithTransport(srv.Client().Transport),
	}

	t.Run("single-platform image", func(t *testing.T) {
		tag, err := goname.NewTag(host+"/test/image:v1", goname.Insecure)
		if err != nil {
			t.Fatalf("new tag: %v", err)
		}
		img, err := random.Image(512, 2)
		if err != nil {
			t.Fatalf("random image: %v", err)
		}
		if err := remote.Write(tag, img, remoteOpts...); err != nil {
			t.Fatalf("push image: %v", err)
		}

		s, err := store.NewLayout(t.TempDir())
		if err != nil {
			t.Fatalf("new layout: %v", err)
		}
		if _, err := s.AddImage(context.Background(), tag.Name(), "", false, "", false, "", remoteOpts...); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		wantOriginalRef := tag.Name()
		found := false
		if err := s.Walk(func(_ string, desc ocispec.Descriptor) error {
			if desc.Annotations[consts.OriginalRefAnnotation] == wantOriginalRef {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if !found {
			t.Errorf("expected an artifact with OriginalRefAnnotation=%q, none found", wantOriginalRef)
		}
	})

	t.Run("multi-platform index", func(t *testing.T) {
		amd64Img, err := random.Image(512, 2)
		if err != nil {
			t.Fatalf("random image amd64: %v", err)
		}
		arm64Img, err := random.Image(512, 2)
		if err != nil {
			t.Fatalf("random image arm64: %v", err)
		}
		idx := mutate.AppendManifests(
			empty.Index,
			mutate.IndexAddendum{
				Add: amd64Img,
				Descriptor: v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Platform:  &v1.Platform{OS: "linux", Architecture: "amd64"},
				},
			},
			mutate.IndexAddendum{
				Add: arm64Img,
				Descriptor: v1.Descriptor{
					MediaType: types.OCIManifestSchema1,
					Platform:  &v1.Platform{OS: "linux", Architecture: "arm64"},
				},
			},
		)
		tag, err := goname.NewTag(host+"/test/multiarch:v1", goname.Insecure)
		if err != nil {
			t.Fatalf("new tag: %v", err)
		}
		if err := remote.WriteIndex(tag, idx, remoteOpts...); err != nil {
			t.Fatalf("push index: %v", err)
		}

		s, err := store.NewLayout(t.TempDir())
		if err != nil {
			t.Fatalf("new layout: %v", err)
		}
		if _, err := s.AddImage(context.Background(), tag.Name(), "", false, "", false, "", remoteOpts...); err != nil {
			t.Fatalf("AddImage: %v", err)
		}

		wantOriginalRef := tag.Name()
		found := false
		if err := s.Walk(func(_ string, desc ocispec.Descriptor) error {
			if desc.Annotations[consts.KindAnnotationName] == consts.KindAnnotationIndex &&
				desc.Annotations[consts.OriginalRefAnnotation] == wantOriginalRef {
				found = true
			}
			return nil
		}); err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if !found {
			t.Errorf("expected the index artifact to have OriginalRefAnnotation=%q, none found", wantOriginalRef)
		}
	})
}

// artifactProbe401Registry serves images normally but returns 401 for cosign
// tag-convention probes and the OCI referrers endpoint.
func artifactProbe401Registry(t *testing.T) (string, []remote.Option) {
	t.Helper()
	inner := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.Contains(p, "/referrers/") ||
			strings.HasSuffix(p, ".sig") || strings.HasSuffix(p, ".att") || strings.HasSuffix(p, ".sbom") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	return host, []remote.Option{remote.WithTransport(srv.Client().Transport)}
}

func TestAddImageArtifactProbe401Fails(t *testing.T) {
	host, opts := artifactProbe401Registry(t)
	seedImage(t, host, "repro/api", "v1", opts...)
	s := newTestStore(t)

	_, err := s.AddImage(context.Background(), host+"/repro/api:v1", "", false, "", false, "", opts...)
	if err == nil {
		t.Fatal("AddImage succeeded despite 401 on artifact probes; want RelatedArtifactError")
	}
	var raErr *store.RelatedArtifactError
	if !errors.As(err, &raErr) {
		t.Fatalf("error %v is not a *store.RelatedArtifactError", err)
	}
}

func TestAddImageArtifactProbe404Succeeds(t *testing.T) {
	// The plain test registry has no sig tags and no referrers: every probe
	// 404s, which must remain "unsigned image", not an error.
	host, opts := newTestRegistry(t)
	seedImage(t, host, "repro/absent", "v1", opts...)
	s := newTestStore(t)

	if _, err := s.AddImage(context.Background(), host+"/repro/absent:v1", "", false, "", false, "", opts...); err != nil {
		t.Fatalf("AddImage failed on 404-only probes: %v", err)
	}
}

func TestIsNotFoundClassification(t *testing.T) {
	// Exercised through the exported surface: a 500-returning registry must
	// fail AddImage the same way 401 does.
	inner := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	opts := []remote.Option{remote.WithTransport(srv.Client().Transport)}
	seedImage(t, host, "repro/err500", "v1", opts...)
	s := newTestStore(t)

	if _, err := s.AddImage(context.Background(), host+"/repro/err500:v1", "", false, "", false, "", opts...); err == nil {
		t.Fatal("AddImage succeeded despite 500 on sig probe")
	}
}

// TestAddImageReferrersEndpoint401Fails exercises referrer-error propagation
// independently of the cosign tag probes (which 404 against the inner
// registry): ggcr v0.22.0's fetchReferrers allows 404/400/406 through to the
// tag fallback but propagates a 401 as *transport.Error.
func TestAddImageReferrersEndpoint401Fails(t *testing.T) {
	inner := registry.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/referrers/") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	opts := []remote.Option{remote.WithTransport(srv.Client().Transport)}
	seedImage(t, host, "repro/ref401", "v1", opts...)
	s := newTestStore(t)

	_, err := s.AddImage(context.Background(), host+"/repro/ref401:v1", "", false, "", false, "", opts...)
	if err == nil {
		t.Fatal("AddImage succeeded despite 401 on the referrers endpoint")
	}
	var raErr *store.RelatedArtifactError
	if !errors.As(err, &raErr) {
		t.Fatalf("error %v is not a *store.RelatedArtifactError", err)
	}
}
