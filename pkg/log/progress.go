package log

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// spinnerFrames is the braille-dot spinner animation, advanced roughly every
// spinnerInterval by the Renderer's ticker goroutine.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 120 * time.Millisecond

// fallbackWidth is used when out is not an *os.File (e.g. tests writing to a
// *bytes.Buffer) or when the terminal width can't be determined.
const fallbackWidth = 80

// heightMargin is reserved off the raw detected terminal height before it is
// used as a row cap anywhere in the Renderer. Writing to a terminal's
// literal last row triggers that terminal's automatic scroll, and a scroll
// silently invalidates the "move cursor up N-1 rows" erase math in
// eraseLocked -- the live region no longer starts where the Renderer thinks
// it does, and the next erase corrupts unrelated scrollback. Reserving one
// row of headroom keeps the region away from the last line so ordinary
// per-frame redraws don't trigger that scroll. Do not remove this margin as
// an "optimization" -- it is load-bearing correctness, not slack.
//
// This does not fully eliminate scroll risk: if the cursor was already near
// the bottom of the terminal before hauler started, there is no way to know
// that from here (no cursor-position query round-trip is performed), and
// this margin does not protect against it. That is a real, acknowledged,
// out-of-scope residual limitation.
const heightMargin = 1

// Renderer renders one live row per in-flight job to a terminal, erasing and
// redrawing the whole live region in place as jobs begin, finish, and log
// lines are emitted through it. A job's row is removed from the region the
// moment it finishes; whatever that job already logged (e.g. its "✓ added
// ..." completion line) stays behind in permanent scrollback, so completed
// jobs "graduate" out of the live region one row at a time while the
// remaining jobs' spinners keep animating below.
//
// Locking: mu is the single, true lock guarding both the terminal (out) and
// all status state (in-flight names, spinner frame, cached terminal
// width/height, drawn row count). The intended composition is to pass the
// Renderer itself as the out argument to log.NewLogger, i.e.
// log.NewLogger(renderer), so NewLogger's own zerolog.SyncWriter wraps the
// Renderer, not the other way around. That gives two lock acquisitions in
// strict nesting order on the log-write path (SyncWriter.mu -> Renderer.mu,
// always in that order, never inverted) -- safe, not a deadlock risk. This
// matters because a second, independent path -- the spinner's ticker
// goroutine -- writes to the terminal via Renderer.mu directly and never
// goes through SyncWriter at all, so Renderer.mu is the only thing actually
// preventing a tick-driven redraw from interleaving with an in-progress
// log-line write. Do NOT restructure this so the Renderer wraps its own
// internal SyncWriter around the real os.Stdout -- that would create a
// second independent lock guarding the same fd with no ordering
// relationship to the first, which is the actual hazard.
type Renderer struct {
	out io.Writer

	mu           sync.Mutex
	started      bool
	stopped      bool
	inFlight     []string
	spinnerFrame int
	width        int
	height       int // cached effective terminal height; 0 == no cap
	drawnRows    int // rows currently occupying screen space from the last draw

	done chan struct{}
	wg   sync.WaitGroup
}

// NewRenderer returns a Renderer that writes to out.
func NewRenderer(out io.Writer) *Renderer {
	return &Renderer{out: out}
}

// Start begins a new progress session and launches the spinner ticker
// goroutine. It caches the terminal width and height for the lifetime of the
// session (mid-run resize is out of scope).
func (r *Renderer) Start() {
	r.mu.Lock()
	r.inFlight = nil
	r.spinnerFrame = 0
	r.width, r.height = r.detectSize()
	r.drawnRows = 0
	r.started = true
	r.stopped = false
	r.done = make(chan struct{})
	r.mu.Unlock()

	r.wg.Add(1)
	go r.run()
}

// Began records name as in-flight and redraws the live region.
func (r *Renderer) Began(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight = append(r.inFlight, name)
	r.redrawLocked()
}

// Finished removes name from the in-flight set and redraws the live region.
// Call it regardless of the job's success or failure so failed jobs are
// removed from the display too.
func (r *Renderer) Finished(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.inFlight {
		if n == name {
			r.inFlight = append(r.inFlight[:i], r.inFlight[i+1:]...)
			break
		}
	}
	r.redrawLocked()
}

// Write implements io.Writer and is the sink log.NewLogger writes through.
// It erases the current live region, writes the log payload untouched, then
// redraws the region on the fresh line(s) left by the payload's trailing
// newline.
func (r *Renderer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.eraseLocked()
	if _, err := r.out.Write(p); err != nil {
		return 0, err
	}
	r.drawRowsLocked()

	return len(p), nil
}

// Stop synchronously halts and joins the spinner goroutine, then erases the
// live region. Safe to call even if Start was never called; a single call is
// sufficient (double-Stop is not required to be safe).
func (r *Renderer) Stop() {
	r.mu.Lock()
	if !r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	close(r.done)
	r.mu.Unlock()

	r.wg.Wait()

	r.mu.Lock()
	r.eraseLocked()
	r.mu.Unlock()
}

// run is the spinner ticker goroutine: it advances the spinner frame and
// redraws roughly every spinnerInterval until Stop closes done.
func (r *Renderer) run() {
	defer r.wg.Done()

	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			r.spinnerFrame = (r.spinnerFrame + 1) % len(spinnerFrames)
			r.redrawLocked()
			r.mu.Unlock()
		}
	}
}

// eraseLocked erases the currently-drawn N-row region (if any) by moving the
// cursor up drawnRows-1 rows, returning to column 0, and clearing from the
// cursor to the end of the screen. Callers must hold mu.
func (r *Renderer) eraseLocked() {
	if r.drawnRows == 0 {
		return
	}
	if r.drawnRows > 1 {
		fmt.Fprintf(r.out, "\x1b[%dA", r.drawnRows-1)
	}
	fmt.Fprint(r.out, "\r\x1b[0J")
	r.drawnRows = 0
}

// drawRowsLocked writes the current set of rows (see buildRowsLocked),
// joined by "\n" with no trailing newline -- cursor ends at the end of the
// last row's content, ready for the next erase. Callers must hold mu.
func (r *Renderer) drawRowsLocked() {
	lines := r.buildRowsLocked()
	if len(lines) == 0 {
		return
	}
	fmt.Fprint(r.out, strings.Join(lines, "\n"))
	r.drawnRows = len(lines)
}

// redrawLocked is eraseLocked + drawRowsLocked, used by Began/Finished/the
// spinner tick. Write uses the two halves separately so the log payload can
// be written in between. Callers must hold mu.
func (r *Renderer) redrawLocked() {
	r.eraseLocked()
	r.drawRowsLocked()
}

// buildRowsLocked returns the text of every row to draw this frame, already
// truncated to width and capped to height (with a "+K more" summary row
// when capped). Callers must hold mu.
func (r *Renderer) buildRowsLocked() []string {
	if len(r.inFlight) == 0 {
		return nil
	}

	frame := spinnerFrames[r.spinnerFrame]

	if r.height <= 0 || len(r.inFlight) <= r.height {
		rows := make([]string, len(r.inFlight))
		for i, ref := range r.inFlight {
			rows[i] = r.formatRowLocked(frame, ref)
		}
		return rows
	}

	shown := r.height - 1
	rows := make([]string, 0, r.height)
	for _, ref := range r.inFlight[:shown] {
		rows = append(rows, r.formatRowLocked(frame, ref))
	}
	remaining := len(r.inFlight) - shown
	rows = append(rows, fmt.Sprintf("  +%d more", remaining))
	return rows
}

// ellipsis marks a truncated ref. It is a 3-byte UTF-8 character, not a
// single byte, so truncation math below sizes against len(ellipsis) rather
// than assuming it costs one unit of budget.
const ellipsis = "…"

// levelWidth is the fixed width of zerolog.ConsoleWriter's rendered level
// field ("DBG", "INF", "WRN", "ERR", ...) -- always 3 characters.
const levelWidth = 3

// alignmentWidth is the number of leading spaces a progress row is padded
// with so it lines up under the log *message* column rather than under the
// timestamp. It is derived from the same pieces that make up one rendered
// log line prefix: consts.CustomTimeFormat, a separating space, the 3-char
// level field, and a second separating space -- i.e.
// "2026-08-01 11:53:30 DBG ". Do not replace this with a hardcoded number;
// if CustomTimeFormat's layout ever changes width, this must move with it.
var alignmentWidth = len(consts.CustomTimeFormat) + 1 + levelWidth + 1

// alignmentPrefix is alignmentWidth worth of spaces, precomputed once.
var alignmentPrefix = strings.Repeat(" ", alignmentWidth)

// formatRowLocked renders "<alignmentPrefix><frame> adding <ref>",
// truncating ref (never the fixed prefix) with a trailing ellipsis so the
// row fits within r.width. Callers must hold mu.
func (r *Renderer) formatRowLocked(frame, ref string) string {
	prefix := alignmentPrefix + frame + " adding "

	budget := r.width - len(prefix)
	if budget < 1 {
		budget = 1
	}
	if len(ref) > budget {
		keep := budget - len(ellipsis)
		if keep < 0 {
			keep = 0
		}
		ref = ref[:keep] + ellipsis
	}

	return prefix + ref
}

// detectSize queries the terminal width and height once via
// golang.org/x/term when out is an *os.File (covers real os.Stdout); it
// falls back to fallbackWidth/0 when out isn't an *os.File (covers
// *bytes.Buffer in tests) or when GetSize errors. Height's fallback is
// deliberately 0 (no cap), not some arbitrary guessed height: an unknown
// height should mean "don't artificially limit rows," not "assume a
// terminal size." See heightMargin for why the detected height is reduced
// by one row before being used as a cap.
func (r *Renderer) detectSize() (width, height int) {
	width = fallbackWidth
	height = 0

	f, ok := r.out.(*os.File)
	if !ok {
		return width, height
	}

	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return width, height
	}
	if w > 0 {
		width = w
	}
	if h > 0 {
		if effective := h - heightMargin; effective >= 1 {
			height = effective
		}
		// else: terminal too small to usefully cap after reserving the
		// margin; leave height at 0 (no cap) rather than a degenerate
		// zero/negative-row region.
	}

	return width, height
}
