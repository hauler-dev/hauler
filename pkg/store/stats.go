package store

import (
	"context"
	"sync/atomic"
)

// ImageStats accumulates layer count and total blob bytes written while
// adding a single image, for cosmetic completion-line reporting in the CLI.
//
// Fields are accumulated with sync/atomic, not plain +=, even though
// today's call graph gives exactly one writer per ImageStats pointer:
// writeImageBlobs computes layer count/size before spawning its per-layer
// errgroup, and writeIndexBlobs iterates an index's child images
// sequentially. That single-writer property is an artifact of
// writeIndexBlobs's loop being sequential today, NOT a structural
// guarantee -- parallelizing that child loop is already a planned next
// step (documented as "trivially safe" once a global semaphore exists,
// which it now does). It is only trivially safe for the blob-writing path;
// it is NOT safe for this pointer under plain arithmetic. Do not simplify
// these back to plain +=/int fields.
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
