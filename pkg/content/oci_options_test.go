package content

// oci_options_test.go covers OCIOption / WithBlobConcurrency, the
// construction-time override of OCI.blobSem's permit count used by
// store.WithBlobConcurrency (in turn driven by `store sync --concurrency`).

import (
	"context"
	"testing"
	"time"
)

// TestWithBlobConcurrency_OverridesPermitCount constructs an OCI with a
// custom blob concurrency of 2 and asserts exactly 2 permits can be acquired
// without blocking, while a 3rd blocks.
func TestWithBlobConcurrency_OverridesPermitCount(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir, WithBlobConcurrency(2))
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	ctx := context.Background()
	if err := o.blobSem.Acquire(ctx, 1); err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if err := o.blobSem.Acquire(ctx, 1); err != nil {
		t.Fatalf("acquire 2: %v", err)
	}

	acquired := make(chan struct{})
	go func() {
		o.blobSem.Acquire(context.Background(), 1) //nolint:errcheck
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("3rd acquire succeeded immediately, want it to block against a 2-permit semaphore")
	case <-time.After(100 * time.Millisecond):
		// expected: still blocked
	}
}

// TestWithBlobConcurrency_ZeroOrNegativeIsNoOp asserts that WithBlobConcurrency
// with n <= 0 leaves the default consts.DefaultBlobConcurrency permit count in
// place, rather than constructing a semaphore with zero (permanently blocked)
// or negative (panicking) capacity.
func TestWithBlobConcurrency_ZeroOrNegativeIsNoOp(t *testing.T) {
	dir := t.TempDir()
	o, err := NewOCI(dir, WithBlobConcurrency(0))
	if err != nil {
		t.Fatalf("NewOCI: %v", err)
	}

	// The default is 16 permits; acquiring one must succeed immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := o.blobSem.Acquire(ctx, 1); err != nil {
		t.Fatalf("acquire against default-capacity semaphore should not block/fail: %v", err)
	}
}

// TestNewOCI_NoOptions_BackwardCompatible asserts the existing no-variadic-arg
// call form still compiles and works after NewOCI gained an opts... parameter.
func TestNewOCI_NoOptions_BackwardCompatible(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewOCI(dir); err != nil {
		t.Fatalf("NewOCI(dir) with no options: %v", err)
	}
}
