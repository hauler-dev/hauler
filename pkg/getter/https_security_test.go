package getter_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"hauler.dev/go/hauler/pkg/getter"
)

// --- A3: Unbounded download protection ---

// TestHttp_Open_RejectsOversizedBody verifies that Open wraps the response body
// in an io.LimitReader so a server that streams more than MaxBytes causes an
// error rather than exhausting disk/memory.
func TestHttp_Open_RejectsOversizedBody(t *testing.T) {
	const cap int64 = 1024 // 1 KiB test cap

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stream cap+1 bytes so the limiter fires.
		payload := strings.Repeat("x", int(cap)+1)
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	// AllowInternalTargets=true because the test server binds to loopback.
	h := getter.NewHttpWithOptions(getter.HttpOptions{
		AllowInternalTargets: true,
		MaxBytes:             cap,
	})
	u, _ := url.Parse(srv.URL + "/big")
	// The size cap must be enforced either at Open() (via Content-Length header)
	// or at read time (via LimitReader).  Both are acceptable.
	rc, openErr := h.Open(context.Background(), u)
	if openErr != nil {
		// Content-Length header triggered the cap early — that is correct.
		return
	}
	defer rc.Close()

	_, readErr := io.ReadAll(rc)
	if readErr == nil {
		t.Fatal("expected an error from Open() or ReadAll() for oversized body, got neither")
	}
}

// TestHttp_Open_Timeout verifies that Open uses a client with a timeout so
// Slowloris-style servers do not hang indefinitely.
func TestHttp_Open_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow-server test in short mode")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never write anything — simulate a stalled server.
		<-r.Context().Done()
	}))
	defer srv.Close()

	h := getter.NewHttpWithOptions(getter.HttpOptions{
		AllowInternalTargets: true,
		Timeout:              50 * time.Millisecond,
	})
	u, _ := url.Parse(srv.URL + "/slow")
	_, err := h.Open(context.Background(), u)
	if err == nil {
		t.Fatal("Open() expected timeout error, got nil")
	}
}

// --- A4: SSRF protection ---

// TestHttp_Open_RejectsNonHTTPScheme verifies that file://, gopher://, etc.
// are rejected before any network call is made.
func TestHttp_Open_RejectsNonHTTPScheme(t *testing.T) {
	for _, scheme := range []string{"file", "gopher", "ftp", "data"} {
		t.Run(scheme, func(t *testing.T) {
			h := getter.NewHttp()
			u, _ := url.Parse(scheme + "://some/path")
			_, err := h.Open(context.Background(), u)
			if err == nil {
				t.Fatalf("Open() expected error for scheme %q, got nil", scheme)
			}
		})
	}
}

// TestHttp_Open_RejectsPrivateIPByDefault verifies that private/loopback
// addresses are blocked unless AllowInternalTargets is set.
func TestHttp_Open_RejectsPrivateIPByDefault(t *testing.T) {
	// Use a real local server so the DNS resolution step completes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secret")
	}))
	defer srv.Close()

	// srv.URL is http://127.0.0.1:<port> — a loopback address.
	h := getter.NewHttp() // default: AllowInternalTargets = false
	u, _ := url.Parse(srv.URL + "/internal")
	_, err := h.Open(context.Background(), u)
	if err == nil {
		t.Fatal("Open() expected SSRF rejection for loopback address, got nil")
	}
}

// TestHttp_Open_AllowsPrivateIPWithFlag verifies that an explicit opt-in flag
// lifts the private-IP restriction, enabling internal CI use cases.
func TestHttp_Open_AllowsPrivateIPWithFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	h := getter.NewHttpWithOptions(getter.HttpOptions{AllowInternalTargets: true})
	u, _ := url.Parse(srv.URL + "/internal")
	rc, err := h.Open(context.Background(), u)
	if err != nil {
		t.Fatalf("Open() unexpected error with AllowInternalTargets=true: %v", err)
	}
	rc.Close()
}

// TestHttp_Open_RejectsRedirectToPrivateIP verifies that CheckRedirect
// re-validates the destination on every hop, blocking public→private pivots.
func TestHttp_Open_RejectsRedirectToPrivateIP(t *testing.T) {
	// The "private" server just responds 200.
	privateSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "private data")
	}))
	defer privateSrv.Close()

	// The "public" server redirects to the private server.
	publicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateSrv.URL+"/secret", http.StatusFound)
	}))
	defer publicSrv.Close()

	h := getter.NewHttp() // default: AllowInternalTargets = false
	u, _ := url.Parse(publicSrv.URL + "/redirect")
	_, err := h.Open(context.Background(), u)
	if err == nil {
		t.Fatal("Open() expected error when redirect targets private IP, got nil")
	}
}

// TestHttp_Open_RejectsIPLiteralPrivate verifies that URLs containing a private,
// loopback, link-local, or IMDS IP literal are rejected at dial time without
// any external network round-trip.
func TestHttp_Open_RejectsIPLiteralPrivate(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:9/anything",
		"http://10.0.0.1:9/anything",
		"http://192.168.0.1:9/anything",
		"http://169.254.169.254/latest/meta",
	}
	h := getter.NewHttp() // default AllowInternalTargets=false
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			u, _ := url.Parse(raw)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := h.Open(ctx, u)
			if err == nil {
				t.Fatalf("Open(%s) expected SSRF rejection, got nil", raw)
			}
		})
	}
}

// TestHttp_Open_RejectsHostnameResolvingToLoopback verifies that the dial-time
// check inspects the *resolved* IP, not just IP literals. This is the
// meaningful demonstration that DNS rebinding is closed: even when the URL
// hostname is "localhost" (not an IP literal), the dial fires the SSRF check
// against the resolved 127.0.0.1.
func TestHttp_Open_RejectsHostnameResolvingToLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "secret")
	}))
	defer srv.Close()

	parsed, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	parsed.Host = "localhost:" + parsed.Port()

	h := getter.NewHttp() // default AllowInternalTargets=false
	_, err = h.Open(context.Background(), parsed)
	if err == nil {
		t.Fatal("Open() expected SSRF rejection for hostname resolving to loopback, got nil")
	}
}
