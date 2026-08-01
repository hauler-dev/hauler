package file

import (
	"context"
	"sync"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"golang.org/x/sync/singleflight"
)

// LayerCache lets multiple File instances that share the same source Path
// (e.g. two manifest entries pointing at the same URL, one plain and one
// with a name override) skip redundant fetches of the underlying content.
// Scope it to the caller (typically one sync run) -- construct with
// NewLayerCache and attach via WithLayerCacheContext.
//
// Sharing the cached gv1.Layer is safe despite differing name overrides:
// compute() always rebuilds its own descriptor and overwrites the Title
// with its own client.Name(Path) afterward, so only the fetch and
// digest/diffID hashing (getter.Client.LayerFrom) is actually shared.
type LayerCache struct {
	group singleflight.Group
	mu    sync.RWMutex
	cache map[string]gv1.Layer
}

// NewLayerCache returns an empty LayerCache.
func NewLayerCache() *LayerCache {
	return &LayerCache{cache: make(map[string]gv1.Layer)}
}

// getOrFetch returns the cached layer for path if present; otherwise it
// calls fetch, sharing the in-flight call across concurrent callers for the
// same path (singleflight) and caching only on success. A failed fetch is
// deliberately not cached: singleflight forgets the flight once it returns,
// so the next call -- a new job or a retry.Operation retry -- starts fresh
// rather than replaying a poisoned error.
func (c *LayerCache) getOrFetch(path string, fetch func() (gv1.Layer, error)) (gv1.Layer, error) {
	c.mu.RLock()
	l, ok := c.cache[path]
	c.mu.RUnlock()
	if ok {
		return l, nil
	}

	v, err, _ := c.group.Do(path, func() (interface{}, error) {
		// Re-check under the flight: another flight for this path may have
		// completed and cached a result while this goroutine queued behind it.
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

// WithLayerCacheContext attaches c to ctx so compute() picks it up via
// layerCacheFromContext -- the same ctx-attached side-channel idiom
// pkg/store uses for ImageStats (store.WithImageStats).
func WithLayerCacheContext(ctx context.Context, c *LayerCache) context.Context {
	return context.WithValue(ctx, layerCacheKey{}, c)
}

// layerCacheFromContext returns the *LayerCache attached via
// WithLayerCacheContext, or nil if none was attached -- the common case for
// callers not coordinating a batch of File instances that might share a
// Path (a single `store add file` call, existing unit tests, etc).
func layerCacheFromContext(ctx context.Context) *LayerCache {
	c, _ := ctx.Value(layerCacheKey{}).(*LayerCache)
	return c
}
