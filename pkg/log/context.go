package log

import "context"

// baseLoggerKey is the context key WithBaseLogger/BaseFromContext use to
// stash an "unadorned" logger.
type baseLoggerKey struct{}

// WithBaseLogger attaches base to ctx as the logger retrievable via
// BaseFromContext -- an "unadorned" logger without whatever contextual
// fields (e.g. a sync job's per-job "image=..." field) the ambient
// FromContext(ctx) logger carries. It exists for log lines that already name
// their own subject inline (a sync job's "✓ added <ref> ..." completion
// line, or any other message that already embeds the same value a per-job
// field would duplicate) so they don't also carry a structured field
// repeating it.
func WithBaseLogger(ctx context.Context, base Logger) context.Context {
	return context.WithValue(ctx, baseLoggerKey{}, base)
}

// BaseFromContext returns the Logger attached via WithBaseLogger, or falls
// back to FromContext(ctx) if none was attached. The fallback matters: it's
// what makes this a no-op (identical output to FromContext) for every caller
// that never had a per-job field to begin with -- store add image's
// single-job path, SyncCmd's per-product-manifest loop, etc.
func BaseFromContext(ctx context.Context) Logger {
	if l, ok := ctx.Value(baseLoggerKey{}).(Logger); ok {
		return l
	}
	return FromContext(ctx)
}
