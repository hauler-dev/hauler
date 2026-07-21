package blob_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"hauler.dev/go/hauler/v2/pkg/blob"
)

// small shared helpers used across all tests in this file.
func newReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

var noTime = time.Time{}

// deterministicBlob returns size bytes and their sha256 v1.Hash.
func deterministicBlob(size int) ([]byte, v1.Hash) {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}
	sum := sha256.Sum256(data)
	return data, v1.Hash{Algorithm: "sha256", Hex: hex.EncodeToString(sum[:])}
}

// rangeServer serves data honoring RFC 7233 Range (via http.ServeContent) and
// answers the /v2/ ping. reqCount counts blob GETs.
func rangeServer(t *testing.T, data []byte, reqCount *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			atomic.AddInt64(reqCount, 1)
			http.ServeContent(w, r, "blob", noTime, newReader(data))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// refFor builds an insecure reference at the given httptest host.
func refFor(t *testing.T, srv *httptest.Server, digest v1.Hash) name.Reference {
	t.Helper()
	host := strings.TrimPrefix(srv.URL, "http://")
	ref, err := name.NewDigest(host+"/test/blob@"+digest.String(), name.Insecure)
	if err != nil {
		t.Fatalf("new digest ref: %v", err)
	}
	return ref
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestFetch_SingleStream_BelowThreshold(t *testing.T) {
	data, h := deterministicBlob(4096)
	var reqs int64
	srv := rangeServer(t, data, &reqs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 1 << 20 // 1 MiB; blob is 4 KiB → single-stream

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, srv, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	if n := atomic.LoadInt64(&reqs); n != 1 {
		t.Fatalf("expected exactly 1 blob GET on single-stream path, got %d", n)
	}
}

func TestFetch_ConnectionsOne_ForcesSingleStream(t *testing.T) {
	data, h := deterministicBlob(300 * 1024)
	var reqs int64
	srv := rangeServer(t, data, &reqs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 64 * 1024 // below blob size...
	opts.Connections = 1            // ...but connections=1 disables chunking

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, srv, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	if n := atomic.LoadInt64(&reqs); n != 1 {
		t.Fatalf("expected exactly 1 blob GET when connections=1, got %d", n)
	}
}

// ignoreRangeServer always returns 200 with the full body, ignoring Range.
func ignoreRangeServer(t *testing.T, data []byte, reqCount *int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			atomic.AddInt64(reqCount, 1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestFetch_Ranged_MultiChunk(t *testing.T) {
	data, h := deterministicBlob(300 * 1024) // 300 KiB
	var reqs int64
	srv := rangeServer(t, data, &reqs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 64 * 1024 // 64 KiB → chunk
	opts.ChunkSize = 64 * 1024      // 5 chunks (4x64 + 1x44)
	opts.Connections = 3

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, srv, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	// 300 KiB / 64 KiB = 5 chunks. The probe (bytes=0-65535) is now a pure
	// capability check with its body discarded, so chunk 0 is re-fetched
	// through the same fetchChunk path as chunks 1-4: 1 probe + 5 chunk GETs
	// (chunks 0-4) = 6 total blob GETs. This trades one extra round-trip for
	// retry-safety on the first chunk (see TestFetch_Ranged_FirstChunkTransientRetry).
	if n := atomic.LoadInt64(&reqs); n != 6 {
		t.Fatalf("expected 6 blob GETs (1 probe + 5 chunks), got %d", n)
	}
}

func TestFetch_Ranged_IgnoredFallback(t *testing.T) {
	data, h := deterministicBlob(300 * 1024)
	var reqs int64
	srv := ignoreRangeServer(t, data, &reqs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 64 * 1024
	opts.ChunkSize = 64 * 1024
	opts.Connections = 3

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, srv, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	// Probe got 200 (whole blob) → exactly one GET, no chunk workers.
	if n := atomic.LoadInt64(&reqs); n != 1 {
		t.Fatalf("expected 1 GET on 200 fallback, got %d", n)
	}
}

func TestFetch_DigestMismatch_DeletesDest(t *testing.T) {
	data, _ := deterministicBlob(4096)
	// Claim a wrong digest so verification must fail.
	wrong := v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("00", 32)}
	var reqs int64
	srv := rangeServer(t, data, &reqs)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 1 << 20 // single-stream

	desc := v1.Descriptor{Digest: wrong, Size: int64(len(data))}
	err := blob.Fetch(context.Background(), refFor(t, srv, wrong), desc, dest, opts)
	if err == nil {
		t.Fatal("expected digest mismatch error, got nil")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("expected dest deleted on mismatch, stat err = %v", statErr)
	}
}

// TestFetch_Redirect_NoAuthForwarded: registry requires a bearer token, then
// 307-redirects the blob GET to an "object storage" host that serves ranges and
// asserts it received NO Authorization header.
//
// go-containerregistry's bearer transport validates the WWW-Authenticate
// realm host and rejects loopback/private addresses UNLESS the realm's
// host:port exactly matches the registry's own host:port (the "same-host
// exception" — see transport/bearer.go's validateRealmURL and
// TestValidateRealmURLSameHost in that package, added for
// https://github.com/google/go-containerregistry/issues/2258). httptest
// servers necessarily listen on 127.0.0.1, so a *separate* token server on a
// different loopback port would trip that guard. To keep this test running
// against a real loopback listener (rather than weakening the assertion),
// the token endpoint is served from a "/token" path on the SAME registry
// server instead of a second httptest.Server — its host:port then matches
// the registry host:port and passes validateRealmURL legitimately. Object
// storage remains a genuinely separate host, so the no-Authorization
// assertion below still proves cross-host redirects strip auth.
func TestFetch_Redirect_NoAuthForwarded(t *testing.T) {
	data, h := deterministicBlob(300 * 1024)

	// Object storage: serves ranges, records whether Authorization arrived.
	var sawAuthOnStorage int64
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			atomic.AddInt64(&sawAuthOnStorage, 1)
		}
		http.ServeContent(w, r, "blob", noTime, newReader(data))
	}))
	defer storage.Close()

	// Registry: /v2/ challenges with Bearer (realm on this same server, see
	// comment above); /token issues a bearer token; blob GET requires the
	// token then redirects cross-host to object storage.
	var registry *httptest.Server
	registry = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer realm="%s/token",service="registry"`, registry.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"sekret"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			if r.Header.Get("Authorization") != "Bearer sekret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, storage.URL+"/blob", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer registry.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = registry.Client().Transport
	opts.ChunkThreshold = 64 * 1024
	opts.ChunkSize = 64 * 1024
	opts.Connections = 3

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, registry, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	if n := atomic.LoadInt64(&sawAuthOnStorage); n != 0 {
		t.Fatalf("Authorization was forwarded to object storage %d time(s); must be 0", n)
	}
}

// TestFetch_TransientRetry: one chunk fails twice then succeeds; per-chunk retry recovers.
func TestFetch_TransientRetry(t *testing.T) {
	data, h := deterministicBlob(200 * 1024)
	var flaky int64 // counts requests to the [64Ki,128Ki) chunk
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			if r.Header.Get("Range") == "bytes=65536-131071" {
				if atomic.AddInt64(&flaky, 1) <= 2 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
			http.ServeContent(w, r, "blob", noTime, newReader(data))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 64 * 1024
	opts.ChunkSize = 64 * 1024
	opts.Connections = 2
	opts.ChunkRetries = 3

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, srv, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	if n := atomic.LoadInt64(&flaky); n < 3 {
		t.Fatalf("expected the flaky chunk to be retried to success (>=3 attempts), got %d", n)
	}
}

// TestFetch_Ranged_FirstChunkTransientRetry: the probe (bytes=0-65535)
// succeeds so ranged() proceeds to parallel assembly, but the *next* two
// requests for that identical byte range fail transiently before succeeding.
//
// Pre-fix, the probe's body is trusted directly for chunk 0 — no further
// request for that range is ever issued, so this scenario can't recover and
// the test fails (only 1 request to the range: the probe).
//
// Post-fix, chunk 0 is fetched through the same fetchChunk path as every
// other chunk: the probe becomes a pure capability check (body discarded)
// and a fresh GET for bytes=0-65535 is retried with backoff like any other
// chunk, recovering from the transient 500s.
func TestFetch_Ranged_FirstChunkTransientRetry(t *testing.T) {
	data, h := deterministicBlob(200 * 1024)
	var flaky int64 // counts requests to the bytes=0-65535 range (probe's range)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			if r.Header.Get("Range") == "bytes=0-65535" {
				// Request 1 = probe: must succeed (206) so ranged() proceeds.
				// Requests 2 and 3 = the first two fetchChunk attempts for
				// chunk 0: fail transiently. Request 4 = the retry that
				// succeeds.
				if count := atomic.AddInt64(&flaky, 1); count == 2 || count == 3 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
			http.ServeContent(w, r, "blob", noTime, newReader(data))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 64 * 1024
	opts.ChunkSize = 64 * 1024
	opts.Connections = 2
	opts.ChunkRetries = 3

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	if err := blob.Fetch(context.Background(), refFor(t, srv, h), desc, dest, opts); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := fileDigest(t, dest); got != h.String() {
		t.Fatalf("digest mismatch: got %s want %s", got, h.String())
	}
	// Expect at least 4 requests to bytes=0-65535: 1 probe + 3 fetchChunk
	// attempts (2 failures + 1 success) for chunk 0.
	if n := atomic.LoadInt64(&flaky); n < 4 {
		t.Fatalf("expected chunk 0 to be independently retried after the probe (>=4 requests to its range), got %d", n)
	}
}

// TestFetch_ContextCancel: cancel mid-fetch → error and dest cleaned up.
func TestFetch_ContextCancel(t *testing.T) {
	data, h := deterministicBlob(2 * 1024 * 1024)
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if strings.Contains(r.URL.Path, "/blobs/") {
			cancel() // cancel as soon as any blob request lands
			http.ServeContent(w, r, "blob", noTime, newReader(data))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "blob")
	opts := blob.DefaultOptions()
	opts.Transport = srv.Client().Transport
	opts.ChunkThreshold = 64 * 1024
	opts.ChunkSize = 64 * 1024
	opts.Connections = 4

	desc := v1.Descriptor{Digest: h, Size: int64(len(data))}
	err := blob.Fetch(ctx, refFor(t, srv, h), desc, dest, opts)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("expected dest cleaned up after cancel, stat err = %v", statErr)
	}
}
