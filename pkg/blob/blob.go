// Package blob downloads OCI blobs to a file as fast as the source registry
// allows, using parallel HTTP Range requests (RFC 7233) when supported and
// falling back to a single stream otherwise. It verifies the sha256 digest of
// the assembled file and never leaves a partial/corrupt file behind.
package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"golang.org/x/sync/errgroup"
)

// Built-in defaults. Kept in this package so pkg/blob stays free of any
// hauler-internal imports.
//
// DefaultConnections and DefaultChunkSize mirror awscli/s3transfer's defaults
// (max_concurrent_requests=10, multipart_chunksize=8MB): more, smaller
// parallel streams tolerate higher-latency/loss-prone paths (e.g. a
// registry-to-S3 redirect over a lossy link) better than fewer, larger ones,
// since each stream carries less unacknowledged data at risk on a stall or
// retry.
const (
	DefaultChunkThreshold int64 = 100 << 20 // 100 MiB
	DefaultConnections          = 10
	DefaultChunkSize      int64 = 8 << 20 // 8 MiB
	DefaultChunkRetries         = 3
)

// Options tunes a Fetch. The zero value is not usable directly; use
// DefaultOptions and override fields. Any field left at its zero value is
// normalized to the corresponding Default* inside Fetch.
type Options struct {
	Keychain       authn.Keychain    // default authn.DefaultKeychain
	Transport      http.RoundTripper // base RT; default remote.DefaultTransport
	ChunkThreshold int64             // min blob size to parallelize
	Connections    int               // worker-pool size; <=1 disables chunking
	ChunkSize      int64             // Range window per request
	ChunkRetries   int               // per-chunk transient retries
}

func DefaultOptions() Options {
	return Options{
		Keychain:       authn.DefaultKeychain,
		Transport:      remote.DefaultTransport,
		ChunkThreshold: DefaultChunkThreshold,
		Connections:    DefaultConnections,
		ChunkSize:      DefaultChunkSize,
		ChunkRetries:   DefaultChunkRetries,
	}
}

func (o *Options) normalize() {
	if o.Keychain == nil {
		o.Keychain = authn.DefaultKeychain
	}
	if o.Transport == nil {
		o.Transport = remote.DefaultTransport
	}
	if o.ChunkThreshold <= 0 {
		o.ChunkThreshold = DefaultChunkThreshold
	}
	if o.Connections == 0 {
		o.Connections = DefaultConnections
	}
	if o.ChunkSize <= 0 {
		o.ChunkSize = DefaultChunkSize
	}
	if o.ChunkRetries == 0 {
		o.ChunkRetries = DefaultChunkRetries
	}
}

// Fetch downloads the blob identified by desc from ref's repository to dest,
// verifying its digest. It parallelizes with Range requests when the source
// supports them and the blob is large enough; otherwise it streams once.
func Fetch(ctx context.Context, ref name.Reference, desc v1.Descriptor, dest string, opts Options) error {
	opts.normalize()

	if desc.Digest.Algorithm != "sha256" {
		return fmt.Errorf("blob: unsupported digest algorithm %q", desc.Digest.Algorithm)
	}

	client, err := buildClient(ctx, ref, opts)
	if err != nil {
		return err
	}
	reg := ref.Context()
	blobURL := fmt.Sprintf("%s://%s/v2/%s/blobs/%s",
		reg.Registry.Scheme(), reg.RegistryStr(), reg.RepositoryStr(), desc.Digest.String())

	// Below threshold or explicit opt-out → today's behavior: one GET, one stream.
	if desc.Size < opts.ChunkThreshold || opts.Connections <= 1 {
		return singleStream(ctx, client, blobURL, dest, desc)
	}
	return ranged(ctx, client, blobURL, dest, desc, opts)
}

// buildClient constructs an authenticated http.Client for ref's registry. The
// bearer transport only attaches Authorization to requests matching the
// registry host, so cross-host redirects to object storage carry no token.
func buildClient(ctx context.Context, ref name.Reference, opts Options) (*http.Client, error) {
	reg := ref.Context().Registry
	auth, err := opts.Keychain.Resolve(reg)
	if err != nil {
		return nil, fmt.Errorf("blob: resolving auth for %s: %w", reg, err)
	}
	scopes := []string{ref.Context().Scope(transport.PullScope)}
	rt, err := transport.NewWithContext(ctx, reg, auth, opts.Transport, scopes)
	if err != nil {
		return nil, fmt.Errorf("blob: building transport for %s: %w", reg, err)
	}
	return &http.Client{Transport: rt}, nil
}

// singleStream performs one plain GET and copies the body to dest, then verifies.
func singleStream(ctx context.Context, client *http.Client, blobURL, dest string, desc v1.Descriptor) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("blob: unexpected status %d fetching %s", resp.StatusCode, blobURL)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(dest)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(dest)
		return closeErr
	}
	if err := verifyDigest(dest, desc.Digest); err != nil {
		os.Remove(dest)
		return err
	}
	return nil
}

// verifyDigest re-reads dest once and compares its sha256 to want.
func verifyDigest(dest string, want v1.Hash) error {
	f, err := os.Open(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if got != want.String() {
		return fmt.Errorf("blob: digest mismatch: got %s want %s", got, want.String())
	}
	return nil
}

// ranged probes with a single ranged GET, then parallelizes the remainder.
func ranged(ctx context.Context, client *http.Client, blobURL, dest string, desc v1.Descriptor, opts Options) error {
	chunkSize := opts.ChunkSize
	firstEnd := chunkSize - 1
	if firstEnd > desc.Size-1 {
		firstEnd = desc.Size - 1
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", firstEnd))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// Server ignored Range: the body IS the whole blob. Stream it — no
		// wasted round-trip. This is the fallback path.
		defer resp.Body.Close()
		f, err := os.Create(dest)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, resp.Body)
		closeErr := f.Close()
		if copyErr != nil {
			os.Remove(dest)
			return copyErr
		}
		if closeErr != nil {
			os.Remove(dest)
			return closeErr
		}
		if err := verifyDigest(dest, desc.Digest); err != nil {
			os.Remove(dest)
			return err
		}
		return nil
	case http.StatusPartialContent:
		// fall through to parallel assembly below
	default:
		resp.Body.Close()
		return fmt.Errorf("blob: unexpected status %d probing range on %s", resp.StatusCode, blobURL)
	}

	// The probe's only job was to confirm the server honors Range with 206.
	// Discard its body rather than trusting it: streaming it in with a bare
	// io.Copy would have no retry and no short-read check, unlike every
	// other chunk. Chunk 0 (bytes=0-firstEnd) is fetched again below through
	// the same fetchChunk path as every other chunk, trading one extra
	// round-trip for retry-safety on the first chunk.
	resp.Body.Close()

	f, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	fail := func(e error) error {
		f.Close()
		os.Remove(dest)
		return e
	}
	if err := f.Truncate(desc.Size); err != nil {
		return fail(err)
	}

	// Fetch every chunk, including chunk 0, across a bounded worker pool.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.Connections)
	for start := int64(0); start < desc.Size; start += chunkSize {
		start := start
		end := start + chunkSize - 1
		if end > desc.Size-1 {
			end = desc.Size - 1
		}
		g.Go(func() error {
			return fetchChunk(gctx, client, blobURL, f, start, end, opts.ChunkRetries)
		})
	}
	if err := g.Wait(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(dest)
		return err
	}
	if err := verifyDigest(dest, desc.Digest); err != nil {
		os.Remove(dest)
		return err
	}
	return nil
}

// fetchChunk GETs one byte range and writes it at its offset, retrying a bounded
// number of times on transient failures. Each attempt re-issues the GET to the
// blob endpoint, so an expired presigned redirect (403) recovers via a fresh
// redirect.
func fetchChunk(ctx context.Context, client *http.Client, blobURL string, f *os.File, start, end int64, retries int) error {
	want := end - start + 1
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		err := func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, blobURL, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusPartialContent {
				return fmt.Errorf("blob: unexpected status %d for range %d-%d", resp.StatusCode, start, end)
			}
			n, err := io.Copy(io.NewOffsetWriter(f, start), resp.Body)
			if err != nil {
				return err
			}
			if n != want {
				return fmt.Errorf("blob: short chunk %d-%d: got %d want %d", start, end, n, want)
			}
			return nil
		}()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		lastErr = err
	}
	return lastErr
}

// backoff returns a short escalating delay between chunk retries.
func backoff(attempt int) time.Duration {
	return time.Duration(attempt) * 200 * time.Millisecond
}
