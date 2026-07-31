package store

// sync_began_order_test.go is a narrow regression test for a single ordering
// bug in runImageJobs' remote-jobs loop (cmd/hauler/cli/store/sync.go):
// progress.Began(name) must fire only once a job's goroutine has actually
// acquired its errgroup.SetLimit semaphore slot and started running, not
// from the calling (outer loop) goroutine before g.Go even attempts to
// acquire that slot. See the runImageJobs doc comment for the
// semaphore-acquisition-order reasoning this test relies on.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// syncBuffer is a mutex-guarded io.Writer + String() buffer. It is
// deliberately a separate lock from the Renderer's own internal mutex, so a
// test goroutine can read the buffer's contents while the Renderer
// concurrently writes to it (from runImageJobs' background goroutine)
// without racing on the underlying storage under `go test -race`.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// blockingManifestHandler delegates every request to next, except that the
// first GET request matching path is held open: it closes entered (so a
// test can deterministically observe that the request has reached the
// server and is now stuck) and then blocks until release is closed. No
// sleeps are involved anywhere in this synchronization.
type blockingManifestHandler struct {
	next    http.Handler
	path    string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *blockingManifestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == h.path {
		h.once.Do(func() { close(h.entered) })
		<-h.release
	}
	h.next.ServeHTTP(w, r)
}

// TestRunImageJobs_BeganFiresOnlyAfterSemaphoreAcquired reproduces the bug
// where progress.Began(name) was invoked from the single-threaded outer
// remote-jobs loop, before g.Go(...) had any chance to block acquiring its
// errgroup.SetLimit concurrency slot.
//
// At concurrency=1 with two jobs, job1's manifest fetch (a real HTTP request
// against a local test registry) is held open on a channel -- indefinitely,
// until the test releases it. Because job2's g.Go call cannot acquire the
// single available slot until job1's goroutine finishes and releases it,
// job2's own goroutine cannot have started yet. So if progress already shows
// job2 as in-flight by the time job1's request is observed to have reached
// the server (which requires job1's goroutine to have done real, comparatively
// expensive network I/O), Began(job2) can only have been called from the
// outer loop rather than from inside job2's own goroutine -- proving the bug
// is present.
func TestRunImageJobs_BeganFiresOnlyAfterSemaphoreAcquired(t *testing.T) {
	// Repo/tag names are kept intentionally short (unlike other tests in this
	// package) so that both refs comfortably fit within the Renderer's status
	// line width budget without being truncated away by truncateNameList --
	// this test asserts on the literal presence of job2's ref substring, so
	// truncation would make that assertion meaningless regardless of whether
	// the underlying bug is present.
	const job1Repo = "a1"
	const job1Tag = "t"
	const job2Repo = "b2"
	const job2Tag = "t"

	handler := &blockingManifestHandler{
		path:    "/v2/" + job1Repo + "/manifests/" + job1Tag,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler.next = registry.New()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	remoteOpts := []remote.Option{remote.WithTransport(srv.Client().Transport)}

	seedImage(t, host, job1Repo, job1Tag, remoteOpts...)
	seedImage(t, host, job2Repo, job2Tag, remoteOpts...)

	s := newTestStore(t)
	buf := &syncBuffer{}
	progress := log.NewRenderer(buf)

	ctx := newTestContext(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	jobs := []imageJob{
		{img: v1.Image{Name: host + "/" + job1Repo + ":" + job1Tag}},
		{img: v1.Image{Name: host + "/" + job2Repo + ":" + job2Tag}},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runImageJobs(ctx, s, jobs, 1, rso, ro, progress)
	}()

	select {
	case <-handler.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for job1's manifest request to reach the test registry")
	}

	if strings.Contains(buf.String(), job2Repo) {
		t.Errorf("progress already shows job2 (%q) as in-flight while job1 is still blocked and concurrency=1 -- "+
			"Began(job2) must have fired from the outer loop before job2's goroutine could possibly have acquired "+
			"a semaphore slot\nbuffer contents:\n%s", job2Repo, buf.String())
	}

	close(handler.release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runImageJobs: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for runImageJobs to finish after releasing job1")
	}

	out := buf.String()
	if got := strings.Count(out, "✓ added"); got != 2 {
		t.Errorf("\"✓ added\" appeared %d times, want 2; full output:\n%s", got, out)
	}
}
