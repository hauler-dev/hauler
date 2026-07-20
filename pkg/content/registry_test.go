package content

// registry_test.go covers the TLS/scheme wiring in NewRegistryHTTPClient()
// and NewRegistryTarget(). It reproduces a v2 regression where
// RegistryOptions.Insecure was a dead field for TLS purposes (the resolver
// always used http.DefaultClient, which has no TLS configuration) and was
// also incorrectly conflated with PlainHTTP when selecting the http/https
// scheme, plus a follow-up regression where Insecure was wired into the
// registry client but not into the Docker Bearer-auth token-fetch client.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewRegistryTarget_InsecureSkipsTLSVerification reproduces the case
// where a registry serves TLS with a self-signed certificate and the caller
// passes --insecure. Before the fix, the resolver's Client was
// http.DefaultClient (no TLS configuration), so the request would fail with
// a certificate verification error even though Insecure was set.
func TestNewRegistryTarget_InsecureSkipsTLSVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "https://")

	opts := RegistryOptions{Insecure: true}
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(opts))

	_, err := target.Resolve(context.Background(), host+"/library/test:latest")
	if err == nil {
		t.Fatalf("expected an error resolving against a fake registry that returns 404, got nil")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "certificate") {
		t.Fatalf("expected no certificate verification error with Insecure: true, got: %v", err)
	}
	// "http://" (not "https://") in the error means Insecure wrongly
	// forced a plain-http dial against this TLS-only server -- the
	// historical conflation of Insecure with PlainHTTP.
	if strings.Contains(err.Error(), "http://") {
		t.Fatalf("expected https scheme to be used (Insecure must not force plain http), got: %v", err)
	}
}

// TestNewRegistryTarget_InsecurePlainHTTPFollowsHTTPSRedirect reproduces the
// user's exact scenario: a registry that serves HTTP but 301-redirects to an
// HTTPS endpoint signed by a private CA. With --insecure --plain-http,
// hauler should dial http, follow the redirect to https, and skip cert
// verification on the redirected request.
func TestNewRegistryTarget_InsecurePlainHTTPFollowsHTTPSRedirect(t *testing.T) {
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer tlsSrv.Close()

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tlsSrv.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer httpSrv.Close()

	host := strings.TrimPrefix(httpSrv.URL, "http://")

	opts := RegistryOptions{Insecure: true, PlainHTTP: true}
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(opts))

	_, err := target.Resolve(context.Background(), host+"/library/test:latest")
	if err == nil {
		t.Fatalf("expected an error resolving against a fake registry that returns 404, got nil")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "certificate signed by unknown authority") || strings.Contains(lower, "certificate") {
		t.Fatalf("expected no certificate verification error after following http->https redirect with Insecure: true, got: %v", err)
	}
}

// TestNewRegistryTarget_InsecureAppliesToBearerTokenFetch reproduces the case
// of a Bearer-auth registry (Harbor, Zot, Distribution + a token server)
// backed by a private CA. The registry responds 401 with a WWW-Authenticate
// Bearer challenge pointing at a token endpoint that is itself self-signed
// TLS. Before the fix, the resolver's TLS-aware client was only wired into
// RegistryHost.Client, never into the docker authorizer -- so the token
// fetch fell back to http.DefaultClient and failed cert verification even
// with Insecure: true. Basic-auth registries never exercise this 401 ->
// token-fetch path, which is why the other tests in this file didn't catch
// it.
func TestNewRegistryTarget_InsecureAppliesToBearerTokenFetch(t *testing.T) {
	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"fake-token"}`))
	}))
	defer tokenSrv.Close()

	registrySrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+tokenSrv.URL+`/token",service="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registrySrv.Close()

	host := strings.TrimPrefix(registrySrv.URL, "https://")

	opts := RegistryOptions{Insecure: true}
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(opts))

	_, err := target.Resolve(context.Background(), host+"/library/test:latest")
	if err == nil {
		t.Fatalf("expected an error resolving against a 401-only fake registry, got nil")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "certificate") {
		t.Fatalf("expected the Bearer token fetch to honor Insecure and skip cert verification, got: %v", err)
	}
}

// TestNewRegistryTarget_SchemeSelection asserts that only PlainHTTP selects
// the http scheme, and that Insecure alone does not force http. This guards
// against the historical conflation of the two flags regressing.
//
// It uses a plain (non-TLS) httptest server as the target. When a resolver
// mistakenly dials https against a plain HTTP server, Go's net/http client
// surfaces the well-known error "http: server gave HTTP response to HTTPS
// client" -- that string's presence/absence tells us which scheme the
// resolver actually used, without needing to expose any internals.
func TestNewRegistryTarget_SchemeSelection(t *testing.T) {
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer plainSrv.Close()

	host := strings.TrimPrefix(plainSrv.URL, "http://")

	tests := []struct {
		name               string
		opts               RegistryOptions
		wantSchemeMismatch bool // true when the resolver dialed https against this plain http server
	}{
		{
			name:               "neither flag set defaults to https",
			opts:               RegistryOptions{},
			wantSchemeMismatch: true,
		},
		{
			name:               "plainHTTP alone selects http",
			opts:               RegistryOptions{PlainHTTP: true},
			wantSchemeMismatch: false,
		},
		{
			name:               "insecure alone does not select http",
			opts:               RegistryOptions{Insecure: true},
			wantSchemeMismatch: true,
		},
		{
			name:               "insecure and plainHTTP together select http",
			opts:               RegistryOptions{Insecure: true, PlainHTTP: true},
			wantSchemeMismatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := NewRegistryTarget(host, tt.opts, NewRegistryHTTPClient(tt.opts))

			_, err := target.Resolve(context.Background(), host+"/library/test:latest")

			gotMismatch := err != nil && strings.Contains(strings.ToLower(err.Error()), "server gave http response to https client")
			if gotMismatch != tt.wantSchemeMismatch {
				t.Fatalf("scheme mismatch detected = %v (err=%v), want %v", gotMismatch, err, tt.wantSchemeMismatch)
			}
		})
	}
}

// TestNewRegistryTarget_PlainHTTPRewritesBearerTokenFetchScheme reproduces the
// exact production failure reported with --plain-http against a Harbor-style
// registry: the registry returns 401 with WWW-Authenticate realm="https://..."
// (always HTTPS), but the server only speaks plain HTTP. Without the scheme
// rewrite in plainHTTPRoundTripper the token fetch fails with:
//
//	"http: server gave HTTP response to HTTPS client"
func TestNewRegistryTarget_PlainHTTPRewritesBearerTokenFetchScheme(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"fake-token"}`))
	}))
	defer tokenSrv.Close()

	// The token server speaks plain HTTP, but we advertise its URL with an
	// https:// scheme in the WWW-Authenticate header -- exactly what Harbor
	// (and similar registries) do regardless of the transport they receive.
	tokenURL := strings.Replace(tokenSrv.URL, "http://", "https://", 1)

	registrySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+tokenURL+`/token",service="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registrySrv.Close()

	host := strings.TrimPrefix(registrySrv.URL, "http://")

	opts := RegistryOptions{PlainHTTP: true}
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(opts))

	_, err := target.Resolve(context.Background(), host+"/library/test:latest")
	// A non-nil error is expected (the fake registry never returns a valid
	// manifest), but it must NOT be the scheme-mismatch error -- that would
	// mean plainHTTPRoundTripper did not rewrite the realm URL.
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "server gave http response to https client") {
		t.Fatalf("PlainHTTP did not rewrite the Bearer token realm URL to http: %v", err)
	}
}

// TestNewRegistryHTTPClient_DoesNotLeakGlobalTLSConfig asserts that building
// an insecure client never mutates http.DefaultTransport in place, which
// would leak InsecureSkipVerify into every other HTTP client in the process.
// It compares http.DefaultTransport.TLSClientConfig before and after
// construction (rather than asserting it is nil) because the Go standard
// library itself lazily populates that field the first time DefaultTransport
// performs a TLS dial elsewhere in the test binary -- the property under
// test is that *construction* leaves it untouched, not that it is globally
// pristine.
func TestNewRegistryHTTPClient_DoesNotLeakGlobalTLSConfig(t *testing.T) {
	dt, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("http.DefaultTransport is not *http.Transport: %T", http.DefaultTransport)
	}
	before := dt.TLSClientConfig

	_ = NewRegistryHTTPClient(RegistryOptions{Insecure: true})

	if dt.TLSClientConfig != before {
		t.Fatalf("NewRegistryHTTPClient mutated the global http.DefaultTransport.TLSClientConfig: before=%+v after=%+v", before, dt.TLSClientConfig)
	}
}

// stubRoundTripper is a minimal http.RoundTripper used to stand in for
// http.DefaultTransport in TestNewRegistryHTTPClient_FallsBackWhenDefaultTransportIsNotHTTPTransport.
// It is deliberately not a *http.Transport, so the comma-ok type assertion in
// NewRegistryHTTPClient takes its "not ok" branch.
type stubRoundTripper struct{}

func (stubRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

// TestNewRegistryHTTPClient_FallsBackWhenDefaultTransportIsNotHTTPTransport
// guards against a panic if something in the process (instrumentation, a
// test harness) has replaced http.DefaultTransport with a RoundTripper that
// isn't *http.Transport. NewRegistryHTTPClient must fall back to a plain
// *http.Transport instead of panicking on the type assertion.
func TestNewRegistryHTTPClient_FallsBackWhenDefaultTransportIsNotHTTPTransport(t *testing.T) {
	original := http.DefaultTransport
	http.DefaultTransport = stubRoundTripper{}
	defer func() { http.DefaultTransport = original }()

	var client *http.Client
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewRegistryHTTPClient panicked with a non-*http.Transport DefaultTransport: %v", r)
			}
		}()
		client = NewRegistryHTTPClient(RegistryOptions{Insecure: true})
	}()

	if client == nil {
		t.Fatalf("expected a non-nil client")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected client.Transport to be *http.Transport, got %T", client.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify to be honored on the fallback transport")
	}
}
