package log

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// wantAlignmentWidth independently recomputes the expected leading-space
// padding on a progress row from the same pieces that make up a rendered log
// line prefix -- consts.CustomTimeFormat, a separating space, the 3-char
// level field, and a second separating space. It deliberately does not
// reference the package's own alignmentWidth var: the point is to catch a
// regression in that var's derivation, not merely to confirm formatRowLocked
// is internally consistent with whatever alignmentWidth happens to hold.
func wantAlignmentWidth() int {
	const wantLevelWidth = 3
	return len(consts.CustomTimeFormat) + 1 + wantLevelWidth + 1
}

// eraseMarker is the tail of every erase sequence eraseLocked writes
// ("\r\x1b[0J", optionally preceded by a "\x1b[NA" cursor-up when more than
// one row was previously drawn). Tests locate the last occurrence of this
// marker to find the region currently visible "on screen" at the point a
// buffer snapshot was taken.
const eraseMarker = "\r\x1b[0J"

// lastDrawnBlock returns everything written after the last erase marker in
// out -- i.e. whatever the Renderer currently has on screen at the moment
// out was captured. An empty result means the live region is empty (no
// in-flight jobs).
func lastDrawnBlock(out string) string {
	idx := strings.LastIndex(out, eraseMarker)
	if idx == -1 {
		return out
	}
	return out[idx+len(eraseMarker):]
}

// safeBuffer is a mutex-guarded io.Writer + String() buffer, deliberately
// using a lock independent of Renderer.mu. The Renderer's background
// spinner goroutine writes to it (via ticks) for the lifetime of the
// session, so any test that reads its contents while a Renderer is still
// running needs its own synchronization to stay race-free under
// `go test -race`; a bare *bytes.Buffer would not be safe for that.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestAlignmentPrefix_DottedGuidePattern verifies that the alignment prefix
// uses a subtle alternating dot-space pattern, has the correct width, is
// ASCII-only, and is precomputed (not dynamically allocated per frame).
func TestAlignmentPrefix_DottedGuidePattern(t *testing.T) {
	// Verify correct width
	if got, want := len(alignmentPrefix), wantAlignmentWidth(); got != want {
		t.Errorf("alignmentPrefix width = %d, want %d", got, want)
	}

	// Verify alternating dot-space pattern
	for i := 0; i < len(alignmentPrefix); i++ {
		want := byte('.')
		if i%2 == 1 {
			want = byte(' ')
		}
		if got := alignmentPrefix[i]; got != want {
			t.Errorf("alignmentPrefix[%d] = %q, want %q", i, got, want)
		}
	}

	// Verify ASCII-only (all bytes < 128)
	for i := 0; i < len(alignmentPrefix); i++ {
		if alignmentPrefix[i] >= 128 {
			t.Errorf("alignmentPrefix[%d] = %d (non-ASCII), want < 128", i, alignmentPrefix[i])
		}
	}

	// Verify the pattern is visually distinct from blank spaces
	if alignmentPrefix == strings.Repeat(" ", len(alignmentPrefix)) {
		t.Errorf("alignmentPrefix should not be all spaces (defeats visual guide purpose)")
	}

	// Document the expected pattern for 24-character width (current default)
	if wantAlignmentWidth() == 24 {
		const want = ". . . . . . . . . . . . "
		if alignmentPrefix != want {
			t.Errorf("alignmentPrefix = %q, want %q", alignmentPrefix, want)
		}
	}
}

// TestRenderer_MultipleBegan_ProducesDistinctRows proves that each
// concurrent in-flight job gets its own row, in insertion order.
func TestRenderer_MultipleBegan_ProducesDistinctRows(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	r.Began("image-a")
	r.Began("image-b")
	r.Began("image-c")

	block := lastDrawnBlock(buf.String())
	rows := strings.Split(block, "\n")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %q", len(rows), block)
	}
	for i, want := range []string{"image-a", "image-b", "image-c"} {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d = %q, want to contain %q", i, rows[i], want)
		}
	}
}

// TestRenderer_Finished_RemovesRowAndShrinksRegion proves Finished removes
// exactly the matching row and that the erase math (cursor-up count) for the
// resulting redraw reflects the smaller, post-shrink row count.
func TestRenderer_Finished_RemovesRowAndShrinksRegion(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	r.Began("image-a")
	r.Began("image-b")

	beforeBlock := lastDrawnBlock(buf.String())
	if got := len(strings.Split(beforeBlock, "\n")); got != 2 {
		t.Fatalf("setup: got %d rows before Finished, want 2: %q", got, beforeBlock)
	}

	snapshot := buf.String()
	r.Finished("image-a")
	after := buf.String()
	newBytes := after[len(snapshot):]

	// 2 rows were on screen prior to this redraw, so the erase must move
	// the cursor up drawnRows-1 == 1 row before clearing.
	if !strings.Contains(newBytes, "\x1b[1A") {
		t.Errorf("expected cursor-up-1 escape reflecting shrink from 2 rows to 1, got %q", newBytes)
	}

	rows := strings.Split(lastDrawnBlock(after), "\n")
	if len(rows) != 1 {
		t.Fatalf("got %d rows after Finished, want 1: %q", len(rows), lastDrawnBlock(after))
	}
	if strings.Contains(rows[0], "image-a") {
		t.Errorf("expected image-a to be removed, got row %q", rows[0])
	}
	if !strings.Contains(rows[0], "image-b") {
		t.Errorf("expected image-b to remain, got row %q", rows[0])
	}
}

// TestRenderer_Write_GraduatesCompletedJobToScrollback proves a completion
// line written via Write appears untouched in scrollback, ahead of the
// region's final redraw with that job's row removed.
func TestRenderer_Write_GraduatesCompletedJobToScrollback(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	r.Began("image-a")
	r.Began("image-b")

	const completionLine = "✓ added image-a (1 layer, 1 B, 0.1s)\n"
	if _, err := r.Write([]byte(completionLine)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r.Finished("image-a")

	out := buf.String()
	if !strings.Contains(out, completionLine) {
		t.Fatalf("expected completion line to appear untouched in scrollback, got %q", out)
	}

	completionIdx := strings.Index(out, completionLine)
	lastMarkerIdx := strings.LastIndex(out, eraseMarker)
	if completionIdx > lastMarkerIdx {
		t.Fatalf("expected completion line to precede the final region redraw")
	}

	rows := strings.Split(lastDrawnBlock(out), "\n")
	if len(rows) != 1 {
		t.Fatalf("got %d rows after graduation, want 1: %q", len(rows), lastDrawnBlock(out))
	}
	if strings.Contains(rows[0], "image-a") {
		t.Errorf("expected image-a's row to be gone after graduation, got %q", rows[0])
	}
	if !strings.Contains(rows[0], "image-b") {
		t.Errorf("expected image-b's row to remain, got %q", rows[0])
	}
}

// TestRenderer_Began_AfterFinished_RegrowsRegion proves the live region
// shrinks to empty when the last in-flight job finishes and grows again when
// a new job begins.
func TestRenderer_Began_AfterFinished_RegrowsRegion(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	r.Began("image-a")
	r.Finished("image-a")

	if block := lastDrawnBlock(buf.String()); block != "" {
		t.Fatalf("expected empty region after last job finished, got %q", block)
	}

	r.Began("image-c")
	rows := strings.Split(lastDrawnBlock(buf.String()), "\n")
	if len(rows) != 1 || !strings.Contains(rows[0], "image-c") {
		t.Errorf("expected region to regrow with image-c, got %q", lastDrawnBlock(buf.String()))
	}
}

// TestRenderer_HeightCap_ShowsSummaryRow proves that once the number of
// in-flight jobs exceeds the (overridden, for determinism) terminal height,
// the region caps at height rows total: height-1 job rows plus one "+K more"
// summary row accounting for the rest.
func TestRenderer_HeightCap_ShowsSummaryRow(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	r.mu.Lock()
	r.height = 3
	r.mu.Unlock()

	names := []string{"image-a", "image-b", "image-c", "image-d", "image-e"}
	for _, n := range names {
		r.Began(n)
	}

	block := lastDrawnBlock(buf.String())
	rows := strings.Split(block, "\n")
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want height cap of 3: %q", len(rows), block)
	}
	for i, want := range names[:2] {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d = %q, want to contain %q", i, rows[i], want)
		}
	}
	last := rows[len(rows)-1]
	if !strings.Contains(last, "+3 more") {
		t.Errorf("expected final row to summarize the remaining 3 jobs, got %q", last)
	}
}

// TestRenderer_TruncatesLongRefToWidth proves a single very long ref is
// truncated with a trailing ellipsis, the fixed "<frame> adding " prefix is
// never truncated, and the total row never exceeds the (overridden, for
// determinism) width.
func TestRenderer_TruncatesLongRefToWidth(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	r.mu.Lock()
	r.width = 60
	r.mu.Unlock()

	longRef := "registry.example.com/some/very/long/path/to/an/image/name:v1.2.3-extra-long-tag"
	r.Began(longRef)

	rows := strings.Split(lastDrawnBlock(buf.String()), "\n")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %q", len(rows), lastDrawnBlock(buf.String()))
	}
	row := rows[0]

	if len(row) > 60 {
		t.Errorf("row length %d exceeds width 60: %q", len(row), row)
	}
	if !strings.HasSuffix(row, "…") {
		t.Errorf("expected truncated row to end with an ellipsis, got %q", row)
	}
	if !strings.HasPrefix(row, alignmentPrefix) {
		t.Errorf("row doesn't start with alignment guide of width %d, got %q", wantAlignmentWidth(), row)
	}
	if len(alignmentPrefix) != wantAlignmentWidth() {
		t.Errorf("alignment guide has width %d, want %d", len(alignmentPrefix), wantAlignmentWidth())
	}

	const prefix = " adding "
	idx := strings.Index(row, prefix)
	if idx == -1 {
		t.Fatalf("expected row to contain the fixed %q prefix, got %q", prefix, row)
	}
	if !strings.HasPrefix(row[idx+len(prefix):], longRef[:10]) {
		t.Errorf("expected truncation to preserve the start of the ref, got %q", row)
	}
}

// TestRenderer_TruncatesRealWorldLongRefAtDefaultWidth exercises
// formatRowLocked with a real production ref reported in a live sync run --
// rgcrprod.azurecr.us/rancher/hardened-addon-resizer:1.8.23-build20260413
// (73 characters) -- against the package's actual default width
// (fallbackWidth, 80 columns; r.width is left untouched here, unlike the
// other truncation test above which overrides it for determinism, since a
// *safeBuffer isn't an *os.File and so already resolves to fallbackWidth).
// Confirms the row renders without panicking or garbling, fits within the
// 80-column budget, and reports whether truncation actually triggered at
// this length.
func TestRenderer_TruncatesRealWorldLongRefAtDefaultWidth(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	defer r.Stop()

	const longRef = "rgcrprod.azurecr.us/rancher/hardened-addon-resizer:1.8.23-build20260413"
	if got := len(longRef); got != 71 {
		t.Fatalf("test setup: longRef is %d characters, want 71", got)
	}

	r.Began(longRef)

	rows := strings.Split(lastDrawnBlock(buf.String()), "\n")
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1: %q", len(rows), lastDrawnBlock(buf.String()))
	}
	row := rows[0]

	if len(row) > fallbackWidth {
		t.Errorf("row length %d exceeds default width %d: %q", len(row), fallbackWidth, row)
	}
	if strings.Contains(row, "\x1b[") {
		t.Errorf("expected the row itself to carry no escape codes, got %q", row)
	}
	if !strings.HasPrefix(row, alignmentPrefix) {
		t.Errorf("row doesn't start with alignment guide of width %d, got %q", wantAlignmentWidth(), row)
	}
	if len(alignmentPrefix) != wantAlignmentWidth() {
		t.Errorf("alignment guide has width %d, want %d", len(alignmentPrefix), wantAlignmentWidth())
	}

	truncated := strings.HasSuffix(row, "…")
	t.Logf("real-world ref %q (%d chars) at default width %d: row = %q (truncated = %v)", longRef, len(longRef), fallbackWidth, row, truncated)

	if truncated {
		if !strings.HasPrefix(row, alignmentPrefix+"⠋ adding "+longRef[:10]) {
			t.Errorf("expected truncation to preserve the start of the ref, got %q", row)
		}
	} else {
		if !strings.Contains(row, longRef) {
			t.Errorf("expected the full ref to appear untruncated, got %q", row)
		}
	}
}

// TestRenderer_Stop_ErasesFinalRegion proves Stop leaves no dangling rows:
// the bytes it writes erase whatever region was last drawn, with no
// replacement draw following.
func TestRenderer_Stop_ErasesFinalRegion(t *testing.T) {
	buf := &safeBuffer{}
	r := NewRenderer(buf)
	r.Start()
	r.Began("image-a")
	r.Began("image-b")

	beforeStop := buf.String()
	r.Stop()
	out := buf.String()
	newBytes := out[len(beforeStop):]

	if !strings.Contains(newBytes, eraseMarker) {
		t.Fatalf("expected Stop to write a final erase sequence, got %q", newBytes)
	}
	// 2 rows were on screen, so the erase must move the cursor up 1 row.
	if !strings.Contains(newBytes, "\x1b[1A") {
		t.Errorf("expected Stop's erase to move the cursor up 1 row (2 rows were drawn), got %q", newBytes)
	}
	if block := lastDrawnBlock(out); block != "" {
		t.Errorf("expected no rows to remain drawn after Stop, got %q", block)
	}
}

// TestRenderer_StopWithoutStart proves Stop is safe to call even when Start
// was never called.
func TestRenderer_StopWithoutStart(t *testing.T) {
	var buf bytes.Buffer
	r := NewRenderer(&buf)
	r.Stop()
}

// TestRenderer_StopJoinsSpinnerGoroutine proves Stop synchronously halts and
// joins the spinner ticker goroutine rather than leaking it. Run with
// -race -count=2 per the task's verification requirements.
func TestRenderer_StopJoinsSpinnerGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		var buf bytes.Buffer
		r := NewRenderer(&buf)
		r.Start()
		r.Began("x")
		r.Stop()
	}

	deadline := time.Now().Add(2 * time.Second)
	var after int
	for {
		after = runtime.NumGoroutine()
		if after <= before+1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if after > before+1 {
		t.Errorf("goroutine count grew from %d to %d after 20 Start/Stop cycles; spinner goroutines appear leaked", before, after)
	}
}

// TestRenderer_DoubleStartIsIdempotent proves a second Start without an
// intervening Stop is a no-op: it must not replace r.done and must not
// launch a second spinner goroutine. Either would strand the first
// goroutine on the channel it already holds -- Stop only ever closes the
// current r.done, so the orphaned goroutine would never see a close and
// Stop's r.wg.Wait() would hang forever. A following Stop must still join
// cleanly.
func TestRenderer_DoubleStartIsIdempotent(t *testing.T) {
	before := runtime.NumGoroutine()

	var buf bytes.Buffer
	r := NewRenderer(&buf)
	r.Start()
	firstDone := r.done

	r.Start()
	if r.done != firstDone {
		t.Errorf("second Start replaced r.done; Start is not idempotent")
	}

	r.Began("x")
	r.Stop()

	deadline := time.Now().Add(2 * time.Second)
	var after int
	for {
		after = runtime.NumGoroutine()
		if after <= before || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if after > before {
		t.Errorf("goroutine count grew from %d to %d after double Start + Stop; second Start leaked a spinner goroutine", before, after)
	}
}
