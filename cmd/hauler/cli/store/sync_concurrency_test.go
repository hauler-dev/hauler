package store

// sync_concurrency_test.go covers the parallelization of runImageJobs'
// remote-image loop (cmd/hauler/cli/store/sync.go): shared-layer
// deduplication across concurrently-synced images, index completeness under
// concurrent AddIndex/SaveIndex calls, error propagation/fail-fast semantics
// through errgroup, and equivalence with the pre-parallelism serial
// behavior at --concurrency 1.

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	gvtypes "github.com/google/go-containerregistry/pkg/v1/types"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/rs/zerolog"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// countingBlobHandler wraps an http.Handler and increments hits[digest] for
// every GET request to /v2/<repo>/blobs/<digest>, then delegates to the
// wrapped handler.
type countingBlobHandler struct {
	next http.Handler
	mu   sync.Mutex
	hits map[string]int
}

func newCountingBlobHandler(next http.Handler) *countingBlobHandler {
	return &countingBlobHandler{next: next, hits: make(map[string]int)}
}

func (c *countingBlobHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if idx := strings.Index(r.URL.Path, "/blobs/"); idx != -1 {
			digest := r.URL.Path[idx+len("/blobs/"):]
			c.mu.Lock()
			c.hits[digest]++
			c.mu.Unlock()
		}
	}
	c.next.ServeHTTP(w, r)
}

func (c *countingBlobHandler) count(digest string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits[digest]
}

// newCountingLocalhostRegistry mirrors newLocalhostRegistry (add_test.go)
// -- listening on "localhost:0" so go-containerregistry auto-selects plain
// HTTP -- but wraps registry.New() in a countingBlobHandler so tests can
// assert on per-digest blob GET counts.
func newCountingLocalhostRegistry(t *testing.T) (host string, remoteOpts []remote.Option, counter *countingBlobHandler) {
	t.Helper()
	counter = newCountingBlobHandler(registry.New())
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("newCountingLocalhostRegistry listen: %v", err)
	}
	srv := httptest.NewUnstartedServer(counter)
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	host = strings.TrimPrefix(srv.URL, "http://")
	remoteOpts = []remote.Option{remote.WithTransport(srv.Client().Transport)}
	return host, remoteOpts, counter
}

// TestSyncImages_SharedLayerDownloadedOnce is the acceptance test for the
// whole parallelization effort: two images that share a common layer are
// synced concurrently, and the shared layer's blob must be fetched from the
// registry exactly once regardless of --concurrency.
func TestSyncImages_SharedLayerDownloadedOnce(t *testing.T) {
	for _, concurrency := range []int{1, 4} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			host, remoteOpts, counter := newCountingLocalhostRegistry(t)

			sharedLayer, err := random.Layer(2048, gvtypes.OCILayer)
			if err != nil {
				t.Fatalf("random.Layer: %v", err)
			}
			sharedDigest, err := sharedLayer.Digest()
			if err != nil {
				t.Fatalf("sharedLayer.Digest: %v", err)
			}

			img1, err := mutate.AppendLayers(empty.Image, sharedLayer)
			if err != nil {
				t.Fatalf("mutate.AppendLayers img1: %v", err)
			}
			img2, err := mutate.AppendLayers(empty.Image, sharedLayer)
			if err != nil {
				t.Fatalf("mutate.AppendLayers img2: %v", err)
			}

			ref1, err := name.NewTag(host+"/repo1:latest", name.Insecure)
			if err != nil {
				t.Fatalf("NewTag ref1: %v", err)
			}
			ref2, err := name.NewTag(host+"/repo2:latest", name.Insecure)
			if err != nil {
				t.Fatalf("NewTag ref2: %v", err)
			}
			if err := remote.Write(ref1, img1, remoteOpts...); err != nil {
				t.Fatalf("remote.Write img1: %v", err)
			}
			if err := remote.Write(ref2, img2, remoteOpts...); err != nil {
				t.Fatalf("remote.Write img2: %v", err)
			}

			s := newTestStore(t)
			ctx := newTestContext(t)
			jobs := []imageJob{
				{img: v1.Image{Name: host + "/repo1:latest"}},
				{img: v1.Image{Name: host + "/repo2:latest"}},
			}
			rso := defaultRootOpts(s.Root)
			ro := defaultCliOpts()

			if err := runImageJobs(ctx, s, jobs, concurrency, rso, ro, nil); err != nil {
				t.Fatalf("runImageJobs: %v", err)
			}

			assertArtifactInStore(t, s, "repo1")
			assertArtifactInStore(t, s, "repo2")

			hits := counter.count(sharedDigest.String())
			if hits != 1 {
				t.Errorf("shared layer blob GET count = %d, want exactly 1 (concurrency=%d)", hits, concurrency)
			}
		})
	}
}

// TestSyncImages_ConcurrencyOneMatchesSerial syncs the same set of images
// into two independent stores, once at concurrency=1 and once at
// concurrency=4, and asserts the resulting stores contain the same set of
// blob digests and the same set of index entries (ref+kind+digest tuples).
//
// Deliberately NOT a byte-for-byte index.json comparison: sync.Map range
// order plus the resolver's SliceStable 2-way merge make exact index.json
// byte order nondeterministic across runs. Comparing sorted logical entry
// sets instead avoids turning this into a flaky golden-file test.
func TestSyncImages_ConcurrencyOneMatchesSerial(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const nImages = 5
	var jobs []imageJob
	for i := 0; i < nImages; i++ {
		repo := fmt.Sprintf("repo%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	sSerial := newTestStore(t)
	sParallel := newTestStore(t)
	ctx := newTestContext(t)
	ro := defaultCliOpts()

	if err := runImageJobs(ctx, sSerial, jobs, 1, defaultRootOpts(sSerial.Root), ro, nil); err != nil {
		t.Fatalf("runImageJobs (concurrency=1): %v", err)
	}
	if err := runImageJobs(ctx, sParallel, jobs, 4, defaultRootOpts(sParallel.Root), ro, nil); err != nil {
		t.Fatalf("runImageJobs (concurrency=4): %v", err)
	}

	serialBlobs := sortedBlobDigests(t, sSerial.Root)
	parallelBlobs := sortedBlobDigests(t, sParallel.Root)
	if !equalStringSlices(serialBlobs, parallelBlobs) {
		t.Errorf("blob digest sets differ:\nserial:   %v\nparallel: %v", serialBlobs, parallelBlobs)
	}

	serialEntries := sortedIndexEntries(t, sSerial)
	parallelEntries := sortedIndexEntries(t, sParallel)
	if !equalStringSlices(serialEntries, parallelEntries) {
		t.Errorf("index entry sets differ:\nserial:   %v\nparallel: %v", serialEntries, parallelEntries)
	}
}

// TestSyncImages_ErrorPropagation covers fail-fast/ignore-errors semantics
// across concurrency levels: 4 good images plus 1 that 404s.
func TestSyncImages_ErrorPropagation(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const nGood = 4
	var goodJobs []imageJob
	for i := 0; i < nGood; i++ {
		repo := fmt.Sprintf("good%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		goodJobs = append(goodJobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}
	badRef := host + "/does-not-exist:latest"

	for _, concurrency := range []int{1, 4} {
		for _, ignoreErrors := range []bool{false, true} {
			t.Run(fmt.Sprintf("concurrency=%d/ignoreErrors=%v", concurrency, ignoreErrors), func(t *testing.T) {
				jobs := append([]imageJob{}, goodJobs...)
				jobs = append(jobs, imageJob{img: v1.Image{Name: badRef}})

				s := newTestStore(t)
				ctx := newTestContext(t)
				rso := defaultRootOpts(s.Root)
				ro := defaultCliOpts()
				ro.IgnoreErrors = ignoreErrors

				err := runImageJobs(ctx, s, jobs, concurrency, rso, ro, nil)

				if !ignoreErrors {
					if err == nil {
						t.Fatal("runImageJobs: expected error, got nil")
					}
					if !strings.Contains(err.Error(), "does-not-exist") {
						t.Errorf("runImageJobs error = %q, want it to identify the bad image (does-not-exist), not a bare cancellation", err.Error())
					}
					return
				}

				if err != nil {
					t.Fatalf("runImageJobs (ignoreErrors=true): unexpected error: %v", err)
				}
				for i := 0; i < nGood; i++ {
					assertArtifactInStore(t, s, fmt.Sprintf("good%d", i))
				}
			})
		}
	}
}

// TestRunImageJobs_CancelledJobsDoNotLogAddingImage reproduces the log-spam
// bug: with concurrency=1 and a bad ref placed before several good refs, the
// bad job's failure cancels the errgroup's derived context. Jobs queued after
// it never get a chance to call s.AddImage, so they must never log the
// "adding image [...] to the store" INFO line either -- otherwise a failed
// sync of 1 image looks like it attempted all of them.
//
// concurrency=1 makes cancellation deterministic: errgroup.SetLimit(1) means
// each g.Go call blocks acquiring its semaphore slot until the previous job's
// goroutine has fully returned (including cancelling gctx on failure), so
// jobs run strictly in slice order and the good jobs are guaranteed to
// observe the already-cancelled context before doing anything.
func TestRunImageJobs_CancelledJobsDoNotLogAddingImage(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const nGood = 3
	badRef := host + "/does-not-exist:latest"
	jobs := []imageJob{{img: v1.Image{Name: badRef}}}
	for i := 0; i < nGood; i++ {
		repo := fmt.Sprintf("good%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	s := newTestStore(t)
	var buf bytes.Buffer
	// "adding image [...]" now logs at Debug (cmd/hauler/cli/store/add.go),
	// so this test needs Debug-level output visible. Per-logger .Level() is
	// not sufficient on its own: zerolog's Logger.should() gates on
	// max(logger.level, zerolog.GlobalLevel()) -- and GlobalLevel is
	// process-global state that other tests in this package mutate (e.g.
	// sync_test.go's TestSyncCmd_DryRun_Products_PrintsManifestToStdout
	// resets it to InfoLevel in a t.Cleanup). Under -shuffle/-count>1 that
	// can leave the global level at Info before this test runs, silently
	// suppressing Debug output regardless of the per-logger Level call. Save
	// and restore the global level around this test so it's deterministic
	// regardless of what ran before it, and doesn't leave Debug-level
	// pollution behind for tests that run after it.
	prevGlobalLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(prevGlobalLevel) })

	zl := zerolog.New(&buf).Level(zerolog.DebugLevel)
	ctx := zl.WithContext(context.Background())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	err := runImageJobs(ctx, s, jobs, 1, rso, ro, nil)
	if err == nil {
		t.Fatal("runImageJobs: expected error, got nil")
	}

	got := strings.Count(buf.String(), "adding image [")
	if got != 1 {
		t.Errorf("\"adding image [\" logged %d times, want exactly 1 (only the failed job should have attempted logging; the %d good jobs queued after it must never start)\nfull log:\n%s", got, nGood, buf.String())
	}
}

// TestSyncImages_IndexCompletenessUnderParallelAdds builds 20 images, syncs
// them at concurrency=8, and asserts the store's on-disk index.json (not
// just the in-memory nameMap) contains all 20 entries after re-opening the
// store fresh.
func TestSyncImages_IndexCompletenessUnderParallelAdds(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const n = 20
	var jobs []imageJob
	for i := 0; i < n; i++ {
		repo := fmt.Sprintf("img%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	s := newTestStore(t)
	ctx := newTestContext(t)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runImageJobs(ctx, s, jobs, 8, rso, ro, nil); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	if got := countArtifactsInStore(t, s); got != n {
		t.Errorf("countArtifactsInStore (same instance) = %d, want %d", got, n)
	}

	reopened, err := store.NewLayout(s.Root)
	if err != nil {
		t.Fatalf("re-opening store: %v", err)
	}
	if got := countArtifactsInStore(t, reopened); got != n {
		t.Errorf("countArtifactsInStore (freshly re-opened store) = %d, want %d -- entries lost from index.json on disk", got, n)
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

func sortedBlobDigests(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, ocispec.ImageBlobsDir, "sha256")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func sortedIndexEntries(t *testing.T, s *store.Layout) []string {
	t.Helper()
	var out []string
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		out = append(out, fmt.Sprintf("%s|%s|%s",
			desc.Annotations[ocispec.AnnotationRefName],
			desc.Annotations["kind"],
			desc.Digest.String(),
		))
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
