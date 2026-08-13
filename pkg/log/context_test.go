package log

// context_test.go covers WithBaseLogger/BaseFromContext, the seam sync.go's
// runImageJobs uses to stash an "unadorned" logger (one without the per-job
// "image=..." field attached to jctx) for lines that already name their own
// subject inline -- see cmd/hauler/cli/store/add.go's storeImage completion
// line for the motivating bug: that field duplicated the ref the message
// already spelled out.

import (
	"context"
	"strings"
	"testing"
)

// TestBaseFromContext_FallsBackToFromContext proves that when no logger was
// ever attached via WithBaseLogger, BaseFromContext behaves identically to
// FromContext. This is what makes the fix a no-op for every caller that
// never had a per-job field to begin with (store add image's single-job
// path, SyncCmd's per-product-manifest loop, etc.) -- they never call
// WithBaseLogger, so BaseFromContext(ctx) == FromContext(ctx) for them.
func TestBaseFromContext_FallsBackToFromContext(t *testing.T) {
	var buf strings.Builder
	base := NewLogger(&buf)
	ctx := base.WithContext(context.Background())

	BaseFromContext(ctx).Infof("hello")

	if got := buf.String(); !strings.Contains(got, "hello") {
		t.Errorf("BaseFromContext fallback did not write through the ambient logger; buf = %q", got)
	}
}

// TestBaseFromContext_StripsFieldsWhileFromContextKeepsThem proves the core
// behavior: given a ctx whose ambient (FromContext) logger carries an
// "image" field, and a base logger (without that field) attached via
// WithBaseLogger, BaseFromContext(ctx) returns the field-less logger while
// FromContext(ctx) on the very same ctx still carries the field. This is
// what lets retry.Operation's attempt-warning lines (which call
// log.FromContext(ctx) themselves, untouched by this change) keep their
// "image=" attribution while storeImage's own ref-naming lines drop the
// duplicate via BaseFromContext.
func TestBaseFromContext_StripsFieldsWhileFromContextKeepsThem(t *testing.T) {
	var buf strings.Builder
	base := NewLogger(&buf)
	withField := base.With(Fields{"image": "example.com/repo:tag"})

	ctx := withField.WithContext(context.Background())
	ctx = WithBaseLogger(ctx, base)

	BaseFromContext(ctx).Infof("unadorned line")
	FromContext(ctx).Infof("adorned line")

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %q", len(lines), out)
	}

	if strings.Contains(lines[0], "image=") {
		t.Errorf("expected BaseFromContext's line to carry no image field, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "image=example.com/repo:tag") {
		t.Errorf("expected FromContext's line to retain the image field, got %q", lines[1])
	}
}
