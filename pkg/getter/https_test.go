package getter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"hauler.dev/go/hauler/v2/pkg/getter"
)

// TestHttp_Open_HonorsContextCancellation proves that Http.Open actually
// wires the ctx argument into the outgoing HTTP request (via
// http.NewRequestWithContext), rather than accepting-but-ignoring it. A
// handler that never writes a response blocks http.Get indefinitely; if Open
// used http.Get instead of a context-aware request, cancelling ctx would
// never unblock the call and this test would hang until killed by the test
// binary's own timeout rather than returning promptly.
func TestHttp_Open_HonorsContextCancellation(t *testing.T) {
	blockCh := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond until the test cleans up... simulates a hung/slow
		// server so the only way Open returns early is via ctx cancellation.
		<-blockCh
	}))
	// Cleanup runs LIFO: unblock the handler goroutine first so srv.Close
	// (registered second, run first) doesn't itself hang waiting for the
	// in-flight handler to return.
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(blockCh) })

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	h := getter.NewHttp(false, "")

	done := make(chan error, 1)
	go func() {
		_, err := h.Open(ctx, u)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from Open after ctx cancellation, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Open did not return within 5s of ctx cancellation... ctx is not wired into the request")
	}
}
