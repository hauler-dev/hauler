package content

// oci_writeblob_singleflight_retry_test.go covers the defensive fix in
// WriteBlob for singleflight.Group.Do handing the flight winner's error to
// every waiter -- including a waiter whose own, independent context is still
// live. See WriteBlob's doc comment. Not reachable via the concurrency
// structure runImageJobs uses today (a single shared errgroup.WithContext
// context per sync invocation), but cheap insurance for any future caller
// that invokes WriteBlob concurrently against independent contexts.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
)

// gatedReader blocks its first Read call on proceed, then returns all of
// data in one shot once released. This lets a test deterministically control
// when the flight winner's io.Copy loop is unblocked relative to a context
// cancellation, without racing Go's scheduler.
type gatedReader struct {
	data    []byte
	proceed <-chan struct{}
	done    bool
}

func (g *gatedReader) Read(p []byte) (int, error) {
	if g.done {
		return 0, io.EOF
	}
	<-g.proceed
	n := copy(p, g.data)
	g.done = true
	return n, nil
}

func (g *gatedReader) Close() error { return nil }

// TestWriteBlob_SingleflightWinnerCancellation_DoesNotFailIndependentWaiter
// is the acceptance test for the retry fix: goroutine 1 (its own, cancellable
// context) wins the singleflight flight for a shared digest and observes its
// own context's cancellation mid-copy. Goroutine 2 (an independent, never
// -cancelled context) joins the same flight as a waiter. Goroutine 1 must
// fail with context.Canceled; goroutine 2 must still succeed, proving the
// shared flight error was not blindly propagated to a caller whose own
// context was never cancelled.
func TestWriteBlob_SingleflightWinnerCancellation_DoesNotFailIndependentWaiter(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir)
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	data := bytes.Repeat([]byte("shared-digest-content"), 20) // small, fits in one io.Copy buffer read
	d := digest.FromBytes(data)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2 := context.Background() // independent, never cancelled

	openCalled := make(chan struct{})
	proceed := make(chan struct{})
	open1 := func() (io.ReadCloser, error) {
		close(openCalled)
		return &gatedReader{data: data, proceed: proceed}, nil
	}
	open2 := func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	errCh1 := make(chan error, 1)
	go func() {
		errCh1 <- o.WriteBlob(ctx1, d, int64(len(data)), open1)
	}()

	select {
	case <-openCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine 1 never became the flight winner (open() was never called)")
	}

	errCh2 := make(chan error, 1)
	go func() {
		// Give goroutine 1 time to register as the singleflight winner before
		// we join as a waiter -- openCalled firing already guarantees this,
		// since singleflight registers the flight before invoking the
		// winner's function.
		errCh2 <- o.WriteBlob(ctx2, d, int64(len(data)), open2)
	}()

	// Let goroutine 2 actually reach o.sf.Do and join the in-flight call.
	time.Sleep(100 * time.Millisecond)

	cancel1()
	close(proceed) // unblock goroutine 1's gatedReader; its next Read observes ctx1 cancellation via ctxReader

	var err1, err2 error
	select {
	case err1 = <-errCh1:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine 1 (flight winner) never returned")
	}
	select {
	case err2 = <-errCh2:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine 2 (independent waiter) never returned")
	}

	if !errors.Is(err1, context.Canceled) {
		t.Errorf("goroutine 1 (flight winner, own ctx cancelled) error = %v, want context.Canceled", err1)
	}
	if err2 != nil {
		t.Errorf("goroutine 2 (independent waiter, own ctx never cancelled) error = %v, want nil", err2)
	}

	blobPath := blobPathFor(dir, d)
	got, readErr := os.ReadFile(blobPath)
	if readErr != nil {
		t.Fatalf("reading blob after retry: %v", readErr)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("blob content = %q, want %q", got, data)
	}
}
