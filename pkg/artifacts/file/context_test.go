package file_test

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"testing"
	"time"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/artifacts/file"
	"hauler.dev/go/hauler/v2/pkg/getter"
)

// blockingGetter's Open blocks on a channel controlled by the test until
// released, unless ctx is cancelled first -- letting tests distinguish
// "the real per-call ctx reached Open" from "compute() built the layer with
// some other, uncancellable ctx" (the context.TODO() bug this file guards
// against).
type blockingGetter struct {
	release chan struct{}
	data    []byte
}

func (g *blockingGetter) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	select {
	case <-g.release:
		return io.NopCloser(bytes.NewReader(g.data)), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g *blockingGetter) Detect(u *url.URL) bool { return true }
func (g *blockingGetter) Name(u *url.URL) string { return "blocked" }
func (g *blockingGetter) Config(u *url.URL) artifacts.Config {
	return artifacts.ToConfig(struct {
		Reference string `json:"reference"`
	}{u.String()}, artifacts.WithConfigMediaType("application/vnd.test.config"))
}

func newBlockingClient(g *blockingGetter) *getter.Client {
	return &getter.Client{
		Options: getter.ClientOptions{},
		Getters: map[string]getter.Getter{"mock": g},
	}
}

// TestFile_WithContext_CancellationAbortsInFlightFetch proves that the ctx
// passed via file.WithContext is the ctx that actually reaches the getter's
// Open call -- not some internal context.TODO() that can never be
// cancelled. Without this wiring, cancelling ctx while compute() is
// blocked inside Open would have no effect and this test would time out.
func TestFile_WithContext_CancellationAbortsInFlightFetch(t *testing.T) {
	g := &blockingGetter{release: make(chan struct{})}
	t.Cleanup(func() { close(g.release) })

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	f := file.NewFile("mock://source", file.WithClient(newBlockingClient(g)), file.WithContext(ctx))

	done := make(chan error, 1)
	go func() {
		_, err := f.Layers()
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from Layers() after ctx cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Layers() did not return within 5s of ctx cancellation... file.WithContext's ctx is not reaching the getter")
	}
}

// TestFile_WithContext_DefaultsToBackground proves NewFile without
// file.WithContext still works end-to-end (the zero-value/default path),
// matching pre-existing behavior for every caller that never sets it.
func TestFile_WithContext_DefaultsToBackground(t *testing.T) {
	f := file.NewFile(filename, file.WithClient(mc))

	if _, err := f.Layers(); err != nil {
		t.Fatalf("Layers() with default context: %v", err)
	}
}
