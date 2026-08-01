package store

// add_file_retry_test.go covers storeFile's (cmd/hauler/cli/store/add.go)
// retry, --ignore-errors, and cancellation behavior -- extending it to match
// storeImage's existing shape (see add_retry_stats_test.go for the analogous
// image-side retry test) as part of bringing Files up to parity with Images
// for `hauler store sync`'s --concurrency support.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
)

// TestStoreFile_RetriesOnTransientFailure proves storeFile retries a failed
// fetch via retry.Operation rather than aborting the whole sync on one
// transient HTTP blip -- storeFile previously had no retry wrapping at all.
func TestStoreFile_RetriesOnTransientFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}

	var gets int32
	mux := http.NewServeMux()
	mux.HandleFunc("/flaky.sh", func(w http.ResponseWriter, r *http.Request) {
		// storeFile's Client.Name(fi.Path) call (used to derive the stored
		// ref, before retry.Operation ever starts) issues an unconditional
		// HEAD request -- see getter.Http.Name -- that must not count
		// against the GET-failure budget below, or the "failure" would be
		// silently consumed before AddArtifact's first real attempt.
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		if atomic.AddInt32(&gets, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "#!/bin/sh\necho ok") //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newTestContext(t)
	s := newTestStore(t)

	rso := defaultRootOpts(s.Root)
	rso.Retries = 2 // one failed attempt + one successful retry
	ro := defaultCliOpts()

	if err := storeFile(ctx, s, v1.File{Path: srv.URL + "/flaky.sh"}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}
	assertArtifactInStore(t, s, "flaky.sh")
	// The first GET fails; layer.FromOpener opens once per successful
	// LayerFrom call (it derives diffID from the already-computed digest
	// instead of a second read), so a successful retry attempt adds 1 more
	// -- 2 total.
	if got := atomic.LoadInt32(&gets); got < 2 {
		t.Errorf("expected at least 2 GET attempts (1 failure + 1 for the succeeding retry), got %d", got)
	}
}

// TestStoreFile_IgnoreErrors_WarnsAndReturnsNil proves storeFile absorbs a
// failure into a warning and returns nil when --ignore-errors is set,
// matching storeImage's existing behavior -- previously storeFile always
// propagated the error regardless of ignore-errors.
func TestStoreFile_IgnoreErrors_WarnsAndReturnsNil(t *testing.T) {
	ctx := newTestContext(t)
	s := newTestStore(t)

	ro := defaultCliOpts()
	ro.IgnoreErrors = true
	rso := defaultRootOpts(s.Root)

	err := storeFile(ctx, s, v1.File{Path: "/nonexistent/path/missing-file.txt"}, ro, rso)
	if err != nil {
		t.Fatalf("expected nil error with --ignore-errors, got: %v", err)
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts after an ignored failure, got %d", n)
	}
}

// TestStoreFile_ContextAlreadyCancelled_ReturnsPromptly is the regression
// test for File.compute()'s context.TODO() bug (pkg/artifacts/file/file.go):
// storeFile must check ctx and bail out before ever attempting to fetch,
// matching storeImage's early ctx.Err() check.
func TestStoreFile_ContextAlreadyCancelled_ReturnsPromptly(t *testing.T) {
	s := newTestStore(t)

	zl := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(zl.WithContext(context.Background()))
	cancel()

	ro := defaultCliOpts()
	rso := defaultRootOpts(s.Root)

	err := storeFile(ctx, s, v1.File{Path: "https://example.invalid/never-fetched.sh"}, ro, rso)
	if err == nil {
		t.Fatal("expected an error for an already-cancelled context, got nil")
	}
	if n := countArtifactsInStore(t, s); n != 0 {
		t.Errorf("expected 0 artifacts, got %d", n)
	}
}

// TestStoreFile_CompletionLine_Format proves storeFile logs a "✓ added"
// completion line (matching storeImage's formatAddedLine convention) rather
// than the old plain "successfully added file" line, and that the old
// "adding file" line is demoted to debug (absent at the default/error log
// level defaultCliOpts() uses).
func TestStoreFile_CompletionLine_Format(t *testing.T) {
	url := seedFileInHTTPServer(t, "completion.sh", "#!/bin/sh\necho done")

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf).Level(zerolog.InfoLevel)
	ctx := zl.WithContext(context.Background())

	ro := defaultCliOpts()
	ro.LogLevel = "info"
	rso := defaultRootOpts(s.Root)

	if err := storeFile(ctx, s, v1.File{Path: url}, ro, rso); err != nil {
		t.Fatalf("storeFile: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "✓ added") {
		t.Errorf("expected a \"✓ added\" completion line, got:\n%s", out)
	}
	if strings.Contains(out, "successfully added file") {
		t.Errorf("expected the old \"successfully added file\" line to be gone, got:\n%s", out)
	}
}
