package file

import (
	"context"
	"sync"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/singleflight"
)

// LayerCache lets multiple File instances that share the same source Path
// (e.g. two Files entries in a manifest pointing at the identical URL, one
// plain and one with a name override) skip redundant network fetches of the
// underlying content. It is scoped to the caller (typically one sync run,
// see cmd/hauler/cli/store's runFileJobs) -- construct one with
// NewLayerCache and attach it to every job's context via
// WithLayerCacheContext.
//
// Sharing the cached gv1.Layer across File instances is safe even though
// each instance may have its own name override: compute() always rebuilds
// its own *v1.Descriptor (via partial.Descriptor) and explicitly overwrites
// the Title annotation with this File's own client.Name(Path) afterward, so
// the descriptor/manifest/title stay correct per-instance regardless of
// which instance actually performed the fetch. Only the expensive part --
// the actual bytes-over-the-wire fetch and digest/diffID hashing done inside
// getter.Client.LayerFrom -- is shared.
type LayerCache struct {
	group singleflight.Group
	mu    sync.RWMutex
	cache map[string]gv1.Layer
}

// NewLayerCache returns an empty LayerCache.
func NewLayerCache() *LayerCache {
	return &LayerCache{cache: make(map[string]gv1.Layer)}
}

// getOrFetch returns the cached layer for path if one exists; otherwise it
// calls fetch, sharing the in-flight call across concurrent callers for the
// same path (singleflight) and caching the result for later callers only on
// success. A failed fetch is deliberately not cached: singleflight forgets
// the flight once it returns, so the next call for the same path -- whether
// a genuinely new job or a retry.Operation retry of the same job -- starts a
// fresh attempt rather than replaying a permanently poisoned error.
func (c *LayerCache) getOrFetch(path string, fetch func() (gv1.Layer, error)) (gv1.Layer, error) {
	c.mu.RLock()
	l, ok := c.cache[path]
	c.mu.RUnlock()
	if ok {
		return l, nil
	}

	v, err, _ := c.group.Do(path, func() (interface{}, error) {
		// Re-check under the flight: another flight for this path may have
		// completed and cached a result while this goroutine was queued
		// behind singleflight, not actually sharing the in-flight call.
		c.mu.RLock()
		l, ok := c.cache[path]
		c.mu.RUnlock()
		if ok {
			return l, nil
		}

		l, err := fetch()
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		c.cache[path] = l
		c.mu.Unlock()
		return l, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(gv1.Layer), nil
}

// layerCacheKey is the context key WithLayerCacheContext/layerCacheFromContext use.
type layerCacheKey struct{}

// WithLayerCacheContext attaches c to ctx so that compute() picks it up
// automatically via layerCacheFromContext -- the same ctx-attached-side-
// channel idiom pkg/store uses for ImageStats (see store.WithImageStats).
func WithLayerCacheContext(ctx context.Context, c *LayerCache) context.Context {
	return context.WithValue(ctx, layerCacheKey{}, c)
}

// layerCacheFromContext returns the *LayerCache attached via
// WithLayerCacheContext, or nil if none was attached -- the common case for
// every caller that isn't coordinating a batch of File instances that might
// share a Path (a single `store add file` call, existing unit tests, etc).
func layerCacheFromContext(ctx context.Context) *LayerCache {
	c, _ := ctx.Value(layerCacheKey{}).(*LayerCache)
	return c
}
