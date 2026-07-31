package store

// sync_progress_test.go covers the live progress display wired into
// runImageJobs/processContent/processImageTxt (cmd/hauler/cli/store/sync.go):
// that a non-nil progress renders escape-coded output alongside the usual
// "✓ added" completion lines, and -- the regression test that matters most
// -- that the real gating path (log.ShouldShowProgress) naturally disables
// progress under go test's non-terminal stdout, producing plain output with
// zero escape bytes.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	v1 "hauler.dev/go/hauler/v2/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// TestRunImageJobs_WithProgress_RendersEscapeCodesAndCompletionLines proves
// that when runImageJobs is given an explicit non-nil progress renderer
// (constructed over a *bytes.Buffer, not real stdout), the resulting output
// contains both the escape-coded erase/redraw sequences and the usual
// "✓ added" completion line for every successful image.
func TestRunImageJobs_WithProgress_RendersEscapeCodesAndCompletionLines(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)

	const n = 3
	var jobs []imageJob
	for i := 0; i < n; i++ {
		repo := fmt.Sprintf("progress%d", i)
		seedImage(t, host, repo, "latest", remoteOpts...)
		jobs = append(jobs, imageJob{img: v1.Image{Name: host + "/" + repo + ":latest"}})
	}

	s := newTestStore(t)
	var buf bytes.Buffer
	zl := zerolog.New(&buf)
	ctx := zl.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	progress := log.NewRenderer(&buf)

	if err := runImageJobs(ctx, s, jobs, 2, rso, ro, progress); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected escape-coded progress output somewhere in the buffer, got %q", out)
	}
	if got := strings.Count(out, "✓ added"); got != n {
		t.Errorf("\"✓ added\" appeared %d times, want %d (one per successful image); full output:\n%s", got, n, out)
	}
}

// TestProcessContent_RealGatingDisablesProgress is the regression test that
// matters most: it runs the real processContent path (not runImageJobs
// directly, and not manually passing progress=nil) against a local test
// registry, with the ambient logger bound to a *bytes.Buffer. Because go
// test's real os.Stdout isn't a TTY, log.ShouldShowProgress naturally
// evaluates false inside processContent's call to newSyncProgress, so
// runImageJobs receives progress == nil through the real gating path.
// Asserts the resulting buffer contains zero escape bytes and still
// contains plain "✓ added" completion lines.
//
// The context is built via log.NewLogger(&buf).WithContext(...) rather than
// a raw zerolog.New(&buf) -- log.NewLogger routes through
// zerolog.ConsoleWriter, which is what actually emitted ANSI color codes
// unconditionally in the bug this test guards against. A raw zerolog logger
// writes plain JSON and never exercises ConsoleWriter at all, so it could
// never have caught this regression.
func TestProcessContent_RealGatingDisablesProgress(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "gated/image", "v1", remoteOpts...)

	manifest := fmt.Sprintf(`apiVersion: content.hauler.cattle.io/v1
kind: Images
metadata:
  name: test-images
spec:
  images:
    - name: %s/gated/image:v1
`, host)

	fi := writeManifestFile(t, manifest)

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processContent(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processContent: %v", err)
	}

	out := buf.Bytes()

	if bytes.Contains(out, []byte("\x1b[")) {
		t.Errorf("expected zero ANSI escape bytes under go test's non-terminal stdout, got %q", out)
	}
	if !strings.Contains(buf.String(), "✓ added") {
		t.Errorf("expected a plain \"✓ added\" completion line, got %q", out)
	}
}

// refCountInLine finds the first line in out containing marker and returns
// how many times ref appears in that line (from marker's start to the line's
// end). Used to prove a completion line names its image ref exactly once --
// not once inline in the message and a second time via a structured
// "image=<ref>" field zerolog would otherwise append from a per-job logger.
func refCountInLine(t *testing.T, out, marker, ref string) int {
	t.Helper()
	idx := strings.Index(out, marker)
	if idx == -1 {
		t.Fatalf("expected output to contain %q, got %q", marker, out)
	}
	line := out[idx:]
	if end := strings.Index(line, "\n"); end != -1 {
		line = line[:end]
	}
	return strings.Count(line, ref)
}

// TestRunImageJobs_NoProgress_CompletionLineRefAppearsOnce is the regression
// test for the reported bug: a sync job's "✓ added <ref> ..." completion
// line named its ref twice -- once inline in the message, once via the
// structured "image=<ref>" field runImageJobs attaches to every job's
// logger for attribution. progress is nil here (the non-TTY case): the bug
// was present in this path too, since the field is attached unconditionally
// regardless of whether progress is active.
func TestRunImageJobs_NoProgress_CompletionLineRefAppearsOnce(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "dup/image", "v1", remoteOpts...)
	ref := host + "/dup/image:v1"

	jobs := []imageJob{{img: v1.Image{Name: ref}}}

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()

	if err := runImageJobs(ctx, s, jobs, 1, rso, ro, nil); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	out := buf.String()
	if got := refCountInLine(t, out, "✓ added", ref); got != 1 {
		t.Errorf("ref %q appeared %d times in the completion line, want 1; full output:\n%s", ref, got, out)
	}
}

// TestRunImageJobs_WithProgress_CompletionLineRefAppearsOnce mirrors
// TestRunImageJobs_NoProgress_CompletionLineRefAppearsOnce but with a
// non-nil progress Renderer, proving the fix applies equally to the
// TTY/progress-enabled path.
func TestRunImageJobs_WithProgress_CompletionLineRefAppearsOnce(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "dup/progress", "v1", remoteOpts...)
	ref := host + "/dup/progress:v1"

	jobs := []imageJob{{img: v1.Image{Name: ref}}}

	s := newTestStore(t)
	var buf bytes.Buffer
	rso := defaultRootOpts(s.Root)
	ro := defaultCliOpts()
	progress := log.NewRenderer(&buf)

	if err := runImageJobs(t.Context(), s, jobs, 1, rso, ro, progress); err != nil {
		t.Fatalf("runImageJobs: %v", err)
	}

	out := buf.String()
	if got := refCountInLine(t, out, "✓ added", ref); got != 1 {
		t.Errorf("ref %q appeared %d times in the completion line, want 1; full output:\n%s", ref, got, out)
	}
}

// TestProcessImageTxt_RealGatingDisablesProgress mirrors
// TestProcessContent_RealGatingDisablesProgress exactly, but drives
// processImageTxt (the image.txt / -i path) instead of processContent (the
// hauler-manifest / -f path). Before this test existed, the -i path's
// gating behavior was only ever confirmed by hand in manual verification
// rounds -- this closes that coverage gap.
func TestProcessImageTxt_RealGatingDisablesProgress(t *testing.T) {
	host, remoteOpts := newTestRegistry(t)
	seedImage(t, host, "gated/txtimage", "v1", remoteOpts...)

	fi := writeImageTxtFile(t, fmt.Sprintf("%s/gated/txtimage:v1\n", host))

	s := newTestStore(t)
	var buf bytes.Buffer
	l := log.NewLogger(&buf)
	ctx := l.WithContext(t.Context())
	o := newSyncOpts(s.Root)
	ro := defaultCliOpts()

	if err := processImageTxt(ctx, fi, o, s, o.StoreRootOpts, ro); err != nil {
		t.Fatalf("processImageTxt: %v", err)
	}

	out := buf.Bytes()

	if bytes.Contains(out, []byte("\x1b[")) {
		t.Errorf("expected zero ANSI escape bytes under go test's non-terminal stdout, got %q", out)
	}
	if !strings.Contains(buf.String(), "✓ added") {
		t.Errorf("expected a plain \"✓ added\" completion line, got %q", out)
	}
}
