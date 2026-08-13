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
	"sync/atomic"
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
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(host, opts))

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
//
// The two httptest servers both listen on 127.0.0.1, on different ports, so
// this also proves that the PlainHTTP https->http rewrite is scoped
// precisely enough to leave the redirect target's scheme alone: a blanket
// rewrite of every outgoing https request (as opposed to one scoped to the
// registry's own host:port) would downgrade this redirect to http and the
// TLS-only redirect target would never be reached.
func TestNewRegistryTarget_InsecurePlainHTTPFollowsHTTPSRedirect(t *testing.T) {
	var tlsReached atomic.Bool
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tlsReached.Store(true)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer tlsSrv.Close()

	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, tlsSrv.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer httpSrv.Close()

	host := strings.TrimPrefix(httpSrv.URL, "http://")

	opts := RegistryOptions{Insecure: true, PlainHTTP: true}
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(host, opts))

	_, err := target.Resolve(context.Background(), host+"/library/test:latest")
	if err == nil {
		t.Fatalf("expected an error resolving against a fake registry that returns 404, got nil")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "certificate signed by unknown authority") || strings.Contains(lower, "certificate") {
		t.Fatalf("expected no certificate verification error after following http->https redirect with Insecure: true, got: %v", err)
	}
	if !tlsReached.Load() {
		t.Fatal("plain-http downgraded a cross-host https redirect: TLS endpoint was never reached")
	}
}

// TestNewRegistryTarget_PlainHTTPRewritesBearerTokenFetchScheme reproduces
// issue #677: a plain-http Bearer-auth registry (e.g. Harbor) that always
// advertises an https:// realm in its WWW-Authenticate challenge, even
// though the registry itself is only reachable over plain http. The token
// realm is on the SAME host:port as the registry (Harbor's own token
// service is co-located behind the same reverse proxy), which is what makes
// the host-scoped rewrite in plainHTTPRoundTripper apply here. Before the
// fix, the containerd Docker authorizer dialed the https realm literally
// and failed with "server gave HTTP response to HTTPS client".
func TestNewRegistryTarget_PlainHTTPRewritesBearerTokenFetchScheme(t *testing.T) {
	var registrySrv *httptest.Server
	registrySrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/service/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"fake-token"}`))
			return
		}
		realm := "https://" + strings.TrimPrefix(registrySrv.URL, "http://") + "/service/token"
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`",service="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registrySrv.Close()

	host := strings.TrimPrefix(registrySrv.URL, "http://")

	opts := RegistryOptions{PlainHTTP: true}
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(host, opts))

	_, err := target.Resolve(context.Background(), host+"/library/test:latest")
	if err == nil {
		t.Fatalf("expected an error resolving against a 401-only fake registry, got nil")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "server gave http response to https client") {
		t.Fatalf("plain-http Bearer token fetch dialed the https realm literally instead of being rewritten to http, got: %v", err)
	}
}

// TestNewRegistryTarget_PlainHTTPRewritesBearerTokenFetchScheme_PathBearingHost
// reproduces the real call path used by `hauler store copy registry://`:
// cmd/hauler/cli/store/copy.go derives its host argument from
// strings.SplitN(targetRef, "://", 2)[1], which for a target reference like
// "oci://harbor:80/library" is "harbor:80/library" -- host:port WITH the
// repo path still attached, not a clean authority. NewRegistryHTTPClient
// must normalize that down to just the authority before comparing against
// req.URL.Host (which is never anything but the authority), or the
// plainHTTPRoundTripper rewrite silently never fires and #677 recurs in
// production even though the "clean host" tests above pass.
func TestNewRegistryTarget_PlainHTTPRewritesBearerTokenFetchScheme_PathBearingHost(t *testing.T) {
	var registrySrv *httptest.Server
	registrySrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/service/token" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"fake-token"}`))
			return
		}
		realm := "https://" + strings.TrimPrefix(registrySrv.URL, "http://") + "/service/token"
		w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`",service="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registrySrv.Close()

	hostPort := strings.TrimPrefix(registrySrv.URL, "http://")
	// Mirrors copy.go's components[1]: host:port with a repo path attached.
	componentsOne := hostPort + "/library"

	opts := RegistryOptions{PlainHTTP: true}
	target := NewRegistryTarget(componentsOne, opts, NewRegistryHTTPClient(componentsOne, opts))

	_, err := target.Resolve(context.Background(), hostPort+"/library/test:latest")
	if err == nil {
		t.Fatalf("expected an error resolving against a 401-only fake registry, got nil")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "server gave http response to https client") {
		t.Fatalf("plain-http Bearer token fetch dialed the https realm literally instead of being rewritten to http (path-bearing host arg like copy.go passes), got: %v", err)
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
	target := NewRegistryTarget(host, opts, NewRegistryHTTPClient(host, opts))

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
			target := NewRegistryTarget(host, tt.opts, NewRegistryHTTPClient(host, tt.opts))

			_, err := target.Resolve(context.Background(), host+"/library/test:latest")

			gotMismatch := err != nil && strings.Contains(strings.ToLower(err.Error()), "server gave http response to https client")
			if gotMismatch != tt.wantSchemeMismatch {
				t.Fatalf("scheme mismatch detected = %v (err=%v), want %v", gotMismatch, err, tt.wantSchemeMismatch)
			}
		})
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

	_ = NewRegistryHTTPClient("registry.example.com", RegistryOptions{Insecure: true})

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
		client = NewRegistryHTTPClient("registry.example.com", RegistryOptions{Insecure: true})
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
