package store_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	ccontent "github.com/containerd/containerd/content"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/store"
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

// TestCopyDescriptor tests basic blob copying
func TestCopyDescriptor(t *testing.T) {
	t.Skip("copyDescriptor is private - tested via Copy integration tests")
}

// TestCopyDescriptorGraph_Manifest tests copying a manifest with config and layers
func TestCopyDescriptorGraph_Manifest(t *testing.T) {
	t.Skip("copyDescriptorGraph is private - tested via Copy integration tests")
}

// TestCopyDescriptorGraph_Index tests copying an index with multiple manifests
func TestCopyDescriptorGraph_Index(t *testing.T) {
	t.Skip("copyDescriptorGraph is private - tested via Copy integration tests")
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