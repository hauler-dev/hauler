package store

import (
	"context"
	"sync/atomic"
)

// ImageStats accumulates layer count and total blob bytes written while
// adding a single image, for cosmetic completion-line reporting in the CLI.
//
// Fields use sync/atomic rather than plain +=: today's call graph gives
// exactly one writer per pointer (writeImageBlobs computes size before its
// per-layer errgroup; writeIndexBlobs iterates children sequentially), but
// that single-writer property is an artifact of writeIndexBlobs's loop
// being sequential today, not a structural guarantee -- parallelizing it is
// a planned next step now that a global blob-write semaphore exists.
type ImageStats struct {
	Layers atomic.Int64
	Bytes  atomic.Int64
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
