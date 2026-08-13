package file_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/artifacts/file"
	"hauler.dev/go/hauler/v2/pkg/getter"
)

// countingGetter counts every Open call and, when failUntil > 0, fails the
// first failUntil calls before succeeding -- used to prove LayerCache
// doesn't permanently poison a path after a failed fetch.
type countingGetter struct {
	data      []byte
	opens     int32
	mu        sync.Mutex
	failCount int
}

func (g *countingGetter) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	atomic.AddInt32(&g.opens, 1)

	g.mu.Lock()
	shouldFail := g.failCount > 0
	if shouldFail {
		g.failCount--
	}
	g.mu.Unlock()

	if shouldFail {
		return nil, errors.New("simulated transient fetch failure")
	}
	return io.NopCloser(bytes.NewReader(g.data)), nil
}

func (g *countingGetter) Detect(u *url.URL) bool { return true }
func (g *countingGetter) Name(u *url.URL) string { return "shared" }
func (g *countingGetter) Config(u *url.URL) artifacts.Config {
	return artifacts.ToConfig(struct {
		Reference string `json:"reference"`
	}{u.String()}, artifacts.WithConfigMediaType("application/vnd.test.config"))
}

func newCountingClient(g *countingGetter, nameOverride string) *getter.Client {
	return &getter.Client{
		Options: getter.ClientOptions{NameOverride: nameOverride},
		Getters: map[string]getter.Getter{"mock": g},
	}
}

// TestLayerCache_DedupesFetchesAcrossFileInstances proves that two File
// instances sharing the same source Path -- e.g. two Files entries pointing
// at the identical URL, one plain and one with a name override, the shape
// testdata/hauler-manifest-pipeline.yaml uses -- fetch the underlying
// content exactly once when they share a *file.LayerCache via
// file.WithLayerCacheContext, even though each independently computes its
// own manifest.
func TestLayerCache_DedupesFetchesAcrossFileInstances(t *testing.T) {
	g := &countingGetter{data: []byte("shared content")}
	cache := file.NewLayerCache()

	baseCtx := file.WithLayerCacheContext(context.Background(), cache)

	f1 := file.NewFile("mock://shared/path", file.WithClient(newCountingClient(g, "")), file.WithContext(baseCtx))
	f2 := file.NewFile("mock://shared/path", file.WithClient(newCountingClient(g, "renamed.sh")), file.WithContext(baseCtx))

	if _, err := f1.Layers(); err != nil {
		t.Fatalf("f1.Layers(): %v", err)
	}
	if _, err := f2.Layers(); err != nil {
		t.Fatalf("f2.Layers(): %v", err)
	}

	if got := atomic.LoadInt32(&g.opens); got != 1 {
		t.Errorf("expected exactly 1 Open call (layer.FromOpener opens once and reuses the digest as diffID) from a single shared fetch, got %d", got)
	}

	// Each instance still computes its own correct Title annotation despite
	// sharing the underlying fetched layer.
	m1, err := f1.Manifest()
	if err != nil {
		t.Fatalf("f1.Manifest(): %v", err)
	}
	m2, err := f2.Manifest()
	if err != nil {
		t.Fatalf("f2.Manifest(): %v", err)
	}
	if got := m1.Layers[0].Annotations["org.opencontainers.image.title"]; got != "shared" {
		t.Errorf("f1 title = %q, want %q", got, "shared")
	}
	if got := m2.Layers[0].Annotations["org.opencontainers.image.title"]; got != "renamed.sh" {
		t.Errorf("f2 title = %q, want %q", got, "renamed.sh")
	}
}

// TestLayerCache_DoesNotPoisonPathAfterFailure proves a failed fetch is not
// cached forever: a second File instance for the same path (simulating a
// retry.Operation retry, which reuses compute()'s memoization only within a
// single File -- a fresh File, as a new job attempt would construct, must
// still be able to succeed once the underlying transient failure clears).
func TestLayerCache_DoesNotPoisonPathAfterFailure(t *testing.T) {
	g := &countingGetter{data: []byte("recovered content"), failCount: 1}
	cache := file.NewLayerCache()
	baseCtx := file.WithLayerCacheContext(context.Background(), cache)

	f1 := file.NewFile("mock://flaky/path", file.WithClient(newCountingClient(g, "")), file.WithContext(baseCtx))
	if _, err := f1.Layers(); err == nil {
		t.Fatal("expected f1.Layers() to fail on the simulated first attempt, got nil error")
	}

	f2 := file.NewFile("mock://flaky/path", file.WithClient(newCountingClient(g, "")), file.WithContext(baseCtx))
	if _, err := f2.Layers(); err != nil {
		t.Fatalf("expected f2.Layers() to succeed after the transient failure cleared, got: %v", err)
	}

	if got := atomic.LoadInt32(&g.opens); got != 2 {
		t.Errorf("expected exactly 2 Open calls (1 failed attempt + 1 for the succeeding attempt, since layer.FromOpener now opens once), got %d", got)
	}
}

// TestLayerCache_NilCacheInContext_FetchesNormally proves compute() falls
// back to a direct, uncached fetch when no LayerCache is attached to ctx --
// the common case (a single `store add file` call, or any File built
// without file.WithLayerCacheContext).
func TestLayerCache_NilCacheInContext_FetchesNormally(t *testing.T) {
	g := &countingGetter{data: []byte("content")}

	f := file.NewFile("mock://uncached/path", file.WithClient(newCountingClient(g, "")))
	if _, err := f.Layers(); err != nil {
		t.Fatalf("Layers(): %v", err)
	}
	if got := atomic.LoadInt32(&g.opens); got != 1 {
		t.Errorf("expected 1 Open call (layer.FromOpener opens once and reuses the digest as diffID), got %d", got)
	}
}

// TestLayerCache_DedupesConcurrentOverlappingFetches proves the dedup also
// holds when two File instances for the same path race concurrently (not
// just sequentially, as TestLayerCache_DedupesFetchesAcrossFileInstances
// exercises) -- the shape a --concurrency > 1 sync run produces when a
// manifest lists the same source twice. Uses a blocking getter to force
// genuine overlap: both goroutines are guaranteed to be inside compute() at
// the same time before either is allowed to finish.
func TestLayerCache_DedupesConcurrentOverlappingFetches(t *testing.T) {
	g := &blockingGetter{release: make(chan struct{}), data: []byte("raced content")}

	cache := file.NewLayerCache()
	baseCtx := file.WithLayerCacheContext(context.Background(), cache)

	var opens int32
	countingOpen := func(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
		atomic.AddInt32(&opens, 1)
		return g.Open(ctx, u)
	}
	countingGetterFn := &fnGetter{open: countingOpen, name: "raced"}

	newClient := func(nameOverride string) *getter.Client {
		return &getter.Client{
			Options: getter.ClientOptions{NameOverride: nameOverride},
			Getters: map[string]getter.Getter{"mock": countingGetterFn},
		}
	}

	f1 := file.NewFile("mock://raced/path", file.WithClient(newClient("")), file.WithContext(baseCtx))
	f2 := file.NewFile("mock://raced/path", file.WithClient(newClient("second.sh")), file.WithContext(baseCtx))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); _, err := f1.Layers(); errs <- err }()
	go func() { defer wg.Done(); _, err := f2.Layers(); errs <- err }()

	// Give both goroutines a chance to actually enter Open (and block on
	// g.release) before releasing them -- proving they were genuinely
	// in-flight concurrently, not accidentally serialized by the test.
	time.Sleep(100 * time.Millisecond)
	close(g.release)

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Layers(): %v", err)
		}
	}

	if got := atomic.LoadInt32(&opens); got != 1 {
		t.Errorf("expected exactly 1 Open call (one shared fetch, opened once by layer.FromOpener) despite two concurrently-racing File instances, got %d", got)
	}
}

// fnGetter adapts a plain Open func into a getter.Getter for tests that need
// to wrap another getter's Open behavior (e.g. to count calls) without
// re-implementing Detect/Name/Config.
type fnGetter struct {
	open func(context.Context, *url.URL) (io.ReadCloser, error)
	name string
}

func (g *fnGetter) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	return g.open(ctx, u)
}
func (g *fnGetter) Detect(u *url.URL) bool { return true }
func (g *fnGetter) Name(u *url.URL) string { return g.name }
func (g *fnGetter) Config(u *url.URL) artifacts.Config {
	return artifacts.ToConfig(struct {
		Reference string `json:"reference"`
	}{u.String()}, artifacts.WithConfigMediaType("application/vnd.test.config"))
}
