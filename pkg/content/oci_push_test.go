package content

// oci_push_test.go covers the atomic/verified blob write path for
// ociPusher.Push, which streams content from containerd's docker resolver
// push path during `hauler store sync`. Unlike content.OCI.WriteBlob (which
// takes a full io.ReadCloser thunk), Push must return a ccontent.Writer that
// the caller streams bytes into over time via Write() calls, then finalizes
// with Close().

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

func TestOCIPusher_Push_NewBlob_AtomicRenameOnSuccess(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := []byte("manifest-or-layer content pushed via the docker resolver path")
	d := digest.FromBytes(data)

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    d,
		Size:      int64(len(data)),
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	blobPath := filepath.Join(dir, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading committed blob: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}

	if info, err := os.Stat(blobPath); err != nil {
		t.Fatalf("stat blob: %v", err)
	} else if info.Mode().Perm() != 0644 {
		t.Errorf("blob mode = %o, want 0644", info.Mode().Perm())
	}

	assertNoTmpFiles(t, dir)
}

func TestOCIPusher_Push_DigestMismatch_DoesNotRenameLeavesNoTmp(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	actual := []byte("the actual bytes streamed into the writer")
	wrongDigest := digest.FromBytes([]byte("not the actual bytes"))

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    wrongDigest,
		Size:      int64(len(actual)),
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := w.Write(actual); err != nil {
		t.Fatalf("Write: %v", err)
	}

	closeErr := w.Close()
	if closeErr == nil {
		t.Fatal("Close: expected digest mismatch error, got nil")
	}
	if !strings.Contains(closeErr.Error(), "digest mismatch") {
		t.Errorf("Close error = %v, want it to mention digest mismatch", closeErr)
	}

	blobPath := filepath.Join(dir, ocispec.ImageBlobsDir, wrongDigest.Algorithm().String(), wrongDigest.Hex())
	if _, statErr := os.Stat(blobPath); !os.IsNotExist(statErr) {
		t.Errorf("final blob path exists after digest mismatch: stat err = %v", statErr)
	}

	assertNoTmpFiles(t, dir)
}

func TestOCIPusher_Push_ExistingBlob_ReturnsDiscardWriterAndLeavesBlobUntouched(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	existing := []byte("blob content that already exists on disk")
	d := digest.FromBytes(existing)
	blobPath := filepath.Join(dir, ocispec.ImageBlobsDir, d.Algorithm().String(), d.Hex())
	if err := os.MkdirAll(filepath.Dir(blobPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(blobPath, existing, 0644); err != nil {
		t.Fatalf("pre-writing blob: %v", err)
	}

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	desc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageLayer,
		Digest:    d,
		Size:      int64(len(existing)),
	}

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	// The docker resolver push path always writes the full content even if
	// the pusher reports it already exists; the discard writer must consume
	// it without error and without touching the existing blob.
	if _, err := w.Write(existing); err != nil {
		t.Fatalf("Write to discard writer: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	got, err := os.ReadFile(blobPath)
	if err != nil {
		t.Fatalf("reading blob: %v", err)
	}
	if !bytes.Equal(got, existing) {
		t.Errorf("existing blob was modified: got %q, want %q", got, existing)
	}
}

// TestOCIPusher_Push_SharesBlobConcurrencyBound proves Push acquires against
// the same o.blobSem that content.OCI.WriteBlob does, per the plan's
// requirement that both write paths share a single process-wide ceiling.
// It saturates blobSem, confirms Push for a not-yet-existing blob blocks
// (returning ctx.Err() once ctx is cancelled while queued) rather than
// bypassing the bound, then releases one permit and confirms Push succeeds
// once a slot is free -- and that Close() releases its own permit in turn.
func TestOCIPusher_Push_SharesBlobConcurrencyBound(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	for i := 0; i < consts.DefaultBlobConcurrency; i++ {
		if err := o.blobSem.Acquire(context.Background(), 1); err != nil {
			t.Fatalf("saturating blobSem: %v", err)
		}
	}

	pusher, err := o.Pusher(context.Background(), "example.com/repo:tag")
	if err != nil {
		t.Fatalf("Pusher: %v", err)
	}

	data := []byte("content pushed while blobSem is fully saturated")
	d := digest.FromBytes(data)
	desc := ocispec.Descriptor{MediaType: ocispec.MediaTypeImageLayer, Digest: d, Size: int64(len(data))}

	// Push must block behind the saturated semaphore: prove it by cancelling
	// a short-lived context while it's queued and confirming Push returns
	// that cancellation rather than silently bypassing the bound.
	shortCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := pusher.Push(shortCtx, desc); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Push against a saturated blobSem with a short-lived context: err = %v, want context.DeadlineExceeded", err)
	}

	// Free exactly one permit, then Push (unbounded ctx this time) must
	// succeed -- proving the earlier block/error was really about the
	// semaphore, not something else broken about Push under saturation.
	o.blobSem.Release(1)

	w, err := pusher.Push(context.Background(), desc)
	if err != nil {
		t.Fatalf("Push after freeing a permit: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close must have released its own permit: acquiring one more should
	// succeed immediately rather than blocking against the still-saturated
	// remainder.
	acquireDone := make(chan error, 1)
	go func() {
		acquireDone <- o.blobSem.Acquire(context.Background(), 1)
	}()
	select {
	case err := <-acquireDone:
		if err != nil {
			t.Fatalf("Acquire after Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blobSem permit was not released by Close")
	}
}

func assertNoTmpFiles(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ocispec.ImageBlobsDir, "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
