package store

// sync_files_test.go covers the Files-side parallelism added to `hauler
// store sync` (cmd/hauler/cli/store/sync.go): resolveFileJobs/runFileJobs,
// mirroring the images-side coverage in sync_test.go and
// sync_progress_test.go (imageJob/resolveImageJobs/runImageJobs).

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/log"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// --------------------------------------------------------------------------
// resolveFileJobs
// --------------------------------------------------------------------------

func TestResolveFileJobs_OneJobPerFile(t *testing.T) {
	files := []v1.File{
		{Path: "https://example.com/a.sh"},
		{Path: "https://example.com/b.sh", Name: "renamed-b.sh"},
	}

	jobs := resolveFileJobs(files)
	if len(jobs) != 2 {
		t.Fatalf("resolveFileJobs: got %d jobs, want 2", len(jobs))
	}
	if jobs[0].file.Path != files[0].Path {
		t.Errorf("jobs[0].file.Path = %q, want %q", jobs[0].file.Path, files[0].Path)
	}
	if jobs[1].file.Name != "renamed-b.sh" {
		t.Errorf("jobs[1].file.Name = %q, want %q", jobs[1].file.Name, "renamed-b.sh")
	}
}

func TestResolveFileJobs_EmptyInput(t *testing.T) {
	jobs := resolveFileJobs(nil)
	if len(jobs) != 0 {
		t.Errorf("resolveFileJobs(nil): got %d jobs, want 0", len(jobs))
	}
}

// --------------------------------------------------------------------------
// runFileJobs
// --------------------------------------------------------------------------

func TestRunFileJobs_AllSucceed(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	url1 := seedFileInHTTPServer(t, "one.sh", "#!/bin/sh\necho one")
	url2 := seedFileInHTTPServer(t, "two.sh", "#!/bin/sh\necho two")

	jobs := resolveFileJobs([]v1.File{{Path: url1}, {Path: url2}})
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runFileJobs(ctx, s, jobs, 2, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}
	assertArtifactInStore(t, s, "one.sh")
	assertArtifactInStore(t, s, "two.sh")
}

// TestRunFileJobs_ConcurrencyOneVsFour_ProduceEquivalentStores proves that
// the resulting store content (blob digests + index entry refs) is
// identical regardless of --concurrency, comparing sorted sets rather than
// raw index.json bytes since ordering is nondeterministic under
// concurrency.
func TestRunFileJobs_ConcurrencyOneVsFour_ProduceEquivalentStores(t *testing.T) {
	ctx := newTestContext(t)

	var urls []string
	for i := 0; i < 6; i++ {
		urls = append(urls, seedFileInHTTPServer(t, fmt.Sprintf("multi-%d.sh", i), fmt.Sprintf("#!/bin/sh\necho %d", i)))
	}
	var files []v1.File
	for _, u := range urls {
		files = append(files, v1.File{Path: u})
	}

	run := func(concurrency int) *storeSnapshot {
		s := newTestStore(t)
		jobs := resolveFileJobs(files)
		rso := defaultRootOpts(s.Root)
		ro := defaultCliOpts()
		if err := runFileJobs(ctx, s, jobs, concurrency, rso, ro, nil); err != nil {
			t.Fatalf("runFileJobs concurrency=%d: %v", concurrency, err)
		}
		return snapshotStore(t, s)
	}

	snap1 := run(1)
	snap4 := run(4)

	if !equalSnapshots(snap1, snap4) {
		t.Errorf("store snapshots differ between concurrency=1 and concurrency=4:\nconcurrency=1: %+v\nconcurrency=4: %+v", snap1, snap4)
	}
}

// storeSnapshot captures the sorted sets that identify a store's content
// independent of index.json's on-disk ordering.
type storeSnapshot struct {
	digests []string
	refs    []string
}

func snapshotStore(t *testing.T, s *store.Layout) *storeSnapshot {
	t.Helper()
	snap := &storeSnapshot{}
	if err := s.OCI.Walk(func(_ string, desc ocispec.Descriptor) error {
		snap.digests = append(snap.digests, desc.Digest.String())
		snap.refs = append(snap.refs, desc.Annotations[ocispec.AnnotationRefName])
		return nil
	}); err != nil {
		t.Fatalf("snapshotStore walk: %v", err)
	}
	sort.Strings(snap.digests)
	sort.Strings(snap.refs)
	return snap
}

func equalSnapshots(a, b *storeSnapshot) bool {
	if len(a.digests) != len(b.digests) || len(a.refs) != len(b.refs) {
		return false
	}
	for i := range a.digests {
		if a.digests[i] != b.digests[i] {
			return false
		}
	}
	for i := range a.refs {
		if a.refs[i] != b.refs[i] {
			return false
		}
	}
	return true
}

// --------------------------------------------------------------------------
// Dedup acceptance test
// --------------------------------------------------------------------------

func TestRunFileJobs_DedupesDuplicateSourceAcrossEntries(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	var requests int32
	mux := http.NewServeMux()
	mux.HandleFunc("/install.sh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		atomic.AddInt32(&requests, 1)
		io.WriteString(w, "#!/bin/sh\necho install") //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	url := srv.URL + "/install.sh"
	files := []v1.File{
		{Path: url},
		{Path: url, Name: "rke2-install.sh"},
	}

	jobs := resolveFileJobs(files)
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runFileJobs(ctx, s, jobs, 4, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}

	assertArtifactInStore(t, s, "install.sh")
	assertArtifactInStore(t, s, "rke2-install.sh")
	if n := countArtifactsInStore(t, s); n != 2 {
		t.Errorf("expected 2 index entries (one per Files entry), got %d", n)
	}
	// Without dedup, 2 manifest entries pointing at the identical source
	// would cost 2x a single file's request count (4, see below) -- one full
	// fetch cycle per entry. The dedup cache (pkg/artifacts/file/cache.go)
	// collapses that to exactly one shared fetch cycle, holding the total to
	// what a single, non-duplicated file already costs: pkg/layer's
	// FromOpener reads the source once up front to compute digest (and
	// reuses it as diffID, see pkg/layer/layer.go), then content.OCI.WriteBlob's
	// own digest-keyed dedup (writeBlobShared) lets exactly one of the two
	// jobs' writeLayer calls actually stream the blob to disk -- 1 + 1 = 2,
	// not 1 and not 4.
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Errorf("expected exactly 2 GET requests (the cost of one file, not two) despite 2 manifest entries sharing the same source, got %d", got)
	}
}

// --------------------------------------------------------------------------
// Error propagation table
// --------------------------------------------------------------------------

func TestRunFileJobs_ErrorPropagation(t *testing.T) {
	tests := []struct {
		name         string
		concurrency  int
		ignoreErrors bool
		wantErr      bool
		wantGoodOK   bool
	}{
		{name: "concurrency=1 ignoreErrors=false", concurrency: 1, ignoreErrors: false, wantErr: true, wantGoodOK: false},
		{name: "concurrency=1 ignoreErrors=true", concurrency: 1, ignoreErrors: true, wantErr: false, wantGoodOK: true},
		{name: "concurrency=4 ignoreErrors=false", concurrency: 4, ignoreErrors: false, wantErr: true, wantGoodOK: false},
		{name: "concurrency=4 ignoreErrors=true", concurrency: 4, ignoreErrors: true, wantErr: false, wantGoodOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newTestContext(t)
			s := newTestStore(t)

			goodURL := seedFileInHTTPServer(t, fmt.Sprintf("good-%s.sh", sanitizeName(tt.name)), "#!/bin/sh\necho good")

			files := []v1.File{
				{Path: "http://127.0.0.1:1/unreachable.sh"},
				{Path: goodURL},
			}

			jobs := resolveFileJobs(files)
			rso := defaultRootOpts(s.Root)
			rso.Retries = 1 // avoid RetriesInterval sleeps in this table
			ro := defaultCliOpts()
			ro.IgnoreErrors = tt.ignoreErrors

			err := runFileJobs(ctx, s, jobs, tt.concurrency, rso, ro, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("runFileJobs error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantGoodOK {
				assertArtifactInStore(t, s, fmt.Sprintf("good-%s.sh", sanitizeName(tt.name)))
			}
		})
	}
}

// sanitizeName turns a subtest name into a usable filename fragment. Lower-
// cased because storeFile's stored ref is normalized to lowercase (OCI/Docker
// reference names must be lowercase) -- an unlowercased fragment here would
// never match a substring check against the actual stored ref.
func sanitizeName(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "-", "=", "-").Replace(s))
}

// --------------------------------------------------------------------------
// Retry
// --------------------------------------------------------------------------

func TestRunFileJobs_RetryEventuallySucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}

	ctx := newTestContext(t)
	s := newTestStore(t)

	var gets int32
	mux := http.NewServeMux()
	mux.HandleFunc("/eventual.sh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		if atomic.AddInt32(&gets, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "#!/bin/sh\necho eventual") //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	jobs := resolveFileJobs([]v1.File{{Path: srv.URL + "/eventual.sh"}})
	rso := defaultRootOpts(s.Root)
	rso.Retries = 2
	ro := defaultCliOpts()

	if err := runFileJobs(ctx, s, jobs, 1, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}
	assertArtifactInStore(t, s, "eventual.sh")
}

// --------------------------------------------------------------------------
// Cancellation
// --------------------------------------------------------------------------

// TestRunFileJobs_CancellationAbortsPromptly is the regression test for
// File.compute()'s context.TODO() fix (pkg/artifacts/file/file.go): a slow
// handler that never responds must not block runFileJobs past ctx's
// cancellation.
func TestRunFileJobs_CancellationAbortsPromptly(t *testing.T) {
	block := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow.sh", func(w http.ResponseWriter, r *http.Request) {
		// storeFile's Client.Name(fi.Path) call (deriving the stored ref,
		// before compute()/fetch even starts) issues an HTTP HEAD via
		// getter.Http.Name, which -- unlike Open -- takes no context
		// parameter at all (net/http.Head has no context-aware variant) and
		// so cannot be cancelled. That's a separate, narrower gap than the
		// one this test targets (compute()'s context.TODO() bug and Open's
		// context wiring); block only the GET that actually fetches content,
		// so this test isolates the fetch-cancellation path under test
		// rather than hanging on the unrelated HEAD gap.
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		<-block
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(block) })

	zl := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(zl.WithContext(context.Background()))
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	s := newTestStore(t)
	jobs := resolveFileJobs([]v1.File{{Path: srv.URL + "/slow.sh"}})
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	done := make(chan error, 1)
	go func() {
		done <- runFileJobs(ctx, s, jobs, 1, rso, ro, nil)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after ctx cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runFileJobs did not return within 5s of ctx cancellation")
	}
}

// --------------------------------------------------------------------------
// Renderer rows
// --------------------------------------------------------------------------

func TestRunFileJobs_WithProgress_RendersEscapeCodesAndCompletionLines(t *testing.T) {
	const n = 3
	var files []v1.File
	for i := 0; i < n; i++ {
		files = append(files, v1.File{Path: seedFileInHTTPServer(t, fmt.Sprintf("progress-%d.sh", i), "#!/bin/sh\necho hi")})
	}

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	ctx := zl.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	progress := log.NewRenderer(&buf)
	jobs := resolveFileJobs(files)

	if err := runFileJobs(ctx, s, jobs, 2, rso, ro, progress); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected escape-coded progress output somewhere in the buffer, got %q", out)
	}
	if got := strings.Count(out, "✓ added"); got != n {
		t.Errorf("\"✓ added\" appeared %d times, want %d; full output:\n%s", got, n, out)
	}
}

func TestRunFileJobs_NoProgress_CompletionLineRefAppearsOnce(t *testing.T) {
	url := seedFileInHTTPServer(t, "dup-ref.sh", "#!/bin/sh\necho dup")

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	jobs := resolveFileJobs([]v1.File{{Path: url}})
	if err := runFileJobs(ctx, s, jobs, 1, rso, ro, nil); err != nil {
		t.Fatalf("runFileJobs: %v", err)
	}

	out := buf.String()
	if got := refCountInLine(t, out, "✓ added", "dup-ref.sh"); got != 1 {
		t.Errorf("ref appeared %d times in the completion line, want 1; full output:\n%s", got, out)
	}
}
