package log_test

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	haulerlog "hauler.dev/go/hauler/pkg/log"
)

// TestCaptureOutput_ConcurrentSafe verifies that concurrent calls to
// CaptureOutput, each writing to os.Stdout from inside fn, do not race on
// os.Stdout/os.Stderr.  The mutex serializes the reassignment window so
// concurrent capturers cannot see each other's writes.  Run with -race to
// catch any residual data race on os.Stdout/os.Stderr.
func TestCaptureOutput_ConcurrentSafe(t *testing.T) {
	const goroutines = 20

	l := haulerlog.NewLogger(io.Discard)

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := haulerlog.CaptureOutput(l, true, func() error {
				_, _ = fmt.Fprintf(os.Stdout, "goroutine-%d-stdout\n", idx)
				_, _ = fmt.Fprintf(os.Stderr, "goroutine-%d-stderr\n", idx)
				return nil
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("CaptureOutput() concurrent call returned error: %v", err)
	}
}

// TestCaptureOutput_RestoresFDsAfterUse confirms that os.Stdout/os.Stderr are
// restored to their original values after CaptureOutput returns, so subsequent
// writes do not target a closed pipe.
func TestCaptureOutput_RestoresFDsAfterUse(t *testing.T) {
	l := haulerlog.NewLogger(io.Discard)
	origStdout := os.Stdout
	origStderr := os.Stderr

	var captured atomic.Bool
	if err := haulerlog.CaptureOutput(l, true, func() error {
		captured.Store(true)
		return nil
	}); err != nil {
		t.Fatalf("CaptureOutput() returned error: %v", err)
	}
	if !captured.Load() {
		t.Fatal("fn was not invoked")
	}

	if os.Stdout != origStdout {
		t.Error("os.Stdout was not restored after CaptureOutput returned")
	}
	if os.Stderr != origStderr {
		t.Error("os.Stderr was not restored after CaptureOutput returned")
	}
}

// TestCaptureOutput_PanicRestoresFDs verifies that a panic inside the
// provided function does not leave os.Stdout/os.Stderr permanently redirected
// and that subsequent calls succeed normally.
func TestCaptureOutput_PanicRestoresFDs(t *testing.T) {
	l := haulerlog.NewLogger(io.Discard)
	origStdout := os.Stdout
	origStderr := os.Stderr

	err := haulerlog.CaptureOutput(l, true, func() error {
		panic("intentional panic for test")
	})
	if err == nil {
		t.Fatal("CaptureOutput() expected error after panic, got nil")
	}

	if os.Stdout != origStdout {
		t.Error("os.Stdout was not restored after CaptureOutput panic")
	}
	if os.Stderr != origStderr {
		t.Error("os.Stderr was not restored after CaptureOutput panic")
	}

	// Run a follow-up capture to confirm the global state is healthy.
	if err := haulerlog.CaptureOutput(l, true, func() error { return nil }); err != nil {
		t.Errorf("subsequent CaptureOutput() failed after panic recovery: %v", err)
	}
}
