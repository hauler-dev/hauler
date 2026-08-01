package log

import "context"

// baseLoggerKey is the context key WithBaseLogger/BaseFromContext use to
// stash an "unadorned" logger.
type baseLoggerKey struct{}

// WithBaseLogger attaches base to ctx as the logger retrievable via
// BaseFromContext -- an "unadorned" logger without whatever contextual
// fields (e.g. a per-job "image=..." field) the ambient FromContext(ctx)
// logger carries. Use it for log lines that already name their subject
// inline (e.g. a sync job's "✓ added <ref> ..." line) so they don't also
// carry a duplicating structured field.
func WithBaseLogger(ctx context.Context, base Logger) context.Context {
	return context.WithValue(ctx, baseLoggerKey{}, base)
}

// BaseFromContext returns the Logger attached via WithBaseLogger, falling
// back to FromContext(ctx) if none was attached -- a no-op for callers that
// never had a per-job field to begin with (store add image's single-job
// path, SyncCmd's per-product-manifest loop, etc.).
func BaseFromContext(ctx context.Context) Logger {
	if l, ok := ctx.Value(baseLoggerKey{}).(Logger); ok {
		return l
	}
	return FromContext(ctx)
}
