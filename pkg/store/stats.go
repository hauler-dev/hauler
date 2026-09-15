package store

import (
	"context"
	"sync/atomic"
)

// ImageStats accumulates layer count and total blob bytes written while
// adding a single image, for cosmetic completion-line reporting in the CLI.
//
// Fields use sync/atomic since Cached/Written are incremented concurrently by writeLayer's per-layer errgroup, same as Layers/Bytes.
type ImageStats struct {
	Layers atomic.Int64
	Bytes  atomic.Int64

	// Cached and Written split Layers into blobs already in the store versus ones actually fetched, for the CLI's completion line.
	Cached  atomic.Int64
	Written atomic.Int64
}

type imageStatsKey struct{}

// WithImageStats attaches s to ctx so that writeImageBlobs (and anything
// else in this package's AddImage call graph) can record layer count/bytes
// into it.
func WithImageStats(ctx context.Context, s *ImageStats) context.Context {
	return context.WithValue(ctx, imageStatsKey{}, s)
}

// imageStatsFromContext returns the *ImageStats attached via WithImageStats,
// or nil if none was attached.
func imageStatsFromContext(ctx context.Context) *ImageStats {
	s, _ := ctx.Value(imageStatsKey{}).(*ImageStats)
	return s
}
