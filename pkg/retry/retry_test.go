package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
)

func testContext() context.Context {
	l := zerolog.New(io.Discard)
	return l.WithContext(context.Background())
}

func TestOperation_SucceedsFirstAttempt(t *testing.T) {
	ctx := testContext()
	rso := &flags.StoreRootOpts{Retries: 1}
	ro := &flags.CliRootOpts{}

	callCount := 0
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		return nil
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}
}

func TestOperation_ExhaustsRetries(t *testing.T) {
	ctx := testContext()
	// Retries=1 → 1 attempt, 0 sleeps (sleep is skipped on last attempt).
	rso := &flags.StoreRootOpts{Retries: 1}
	ro := &flags.CliRootOpts{}

	callCount := 0
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		return fmt.Errorf("always fails")
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}
	want := fmt.Sprintf("operation unsuccessful after %d attempts: always fails", 1)
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestOperation_RetriesAndSucceeds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires one RetriesInterval sleep (5s)")
	}
	ctx := testContext()
	// Retries=2: fails on attempt 1, succeeds on attempt 2 (one 5s sleep).
	rso := &flags.StoreRootOpts{Retries: 2}
	ro := &flags.CliRootOpts{}

	callCount := 0
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		if callCount < 2 {
			return fmt.Errorf("transient error")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("expected success on retry, got: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestOperation_DefaultRetries(t *testing.T) {
	ctx := testContext()
	// Retries=0 → falls back to consts.DefaultRetries (3).
	// Verify happy path (success first attempt) is unaffected.
	rso := &flags.StoreRootOpts{Retries: 0}
	ro := &flags.CliRootOpts{}

	callCount := 0
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}

	// Exhausting all default retries requires (DefaultRetries-1) sleeps of 5s each.
	// Only run this sub-test in non-short mode.
	t.Run("FailAllWithDefault", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping: requires (DefaultRetries-1) * 5s sleeps")
		}
		rso2 := &flags.StoreRootOpts{Retries: 0}
		ro2 := &flags.CliRootOpts{}
		callCount2 := 0
		err2 := Operation(ctx, rso2, ro2, func() error {
			callCount2++
			return fmt.Errorf("fail")
		})
		if err2 == nil {
			t.Fatal("expected error, got nil")
		}
		if callCount2 != consts.DefaultRetries {
			t.Fatalf("expected %d calls (DefaultRetries), got %d", consts.DefaultRetries, callCount2)
		}
		want := fmt.Sprintf("operation unsuccessful after %d attempts: fail", consts.DefaultRetries)
		if err2.Error() != want {
			t.Fatalf("error = %q, want %q", err2.Error(), want)
		}
	})
}

func TestOperation_EnvVar_IgnoreErrors(t *testing.T) {
	ctx := testContext()
	// Retries=1 → 1 attempt, no sleep.
	rso := &flags.StoreRootOpts{Retries: 1}
	ro := &flags.CliRootOpts{IgnoreErrors: false}

	t.Setenv(consts.HaulerIgnoreErrors, "true")

	// Confirm the pure helper observes the env var without needing Operation to
	// mutate anything first.
	if !flags.ShouldIgnoreErrors(ro) {
		t.Fatal("expected ShouldIgnoreErrors(ro)=true once env var is set")
	}

	callCount := 0
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		return fmt.Errorf("some error")
	})

	// IgnoreErrors controls logging severity (WARN instead of ERR) — it does NOT
	// suppress error returns. Operation always returns an error after exhausting
	// all retries regardless of this flag (see pkg/retry/retry.go).
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	// Operation must NOT mutate ro — the env var override is read via the pure
	// flags.ShouldIgnoreErrors helper on every call instead of being cached back
	// onto the shared *CliRootOpts (that mutation was a data race once callers
	// run concurrently).
	if ro.IgnoreErrors {
		t.Fatal("expected ro.IgnoreErrors to remain false: Operation must not mutate ro")
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}
}

func TestOperation_ContextCancelledDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(testContext())
	rso := &flags.StoreRootOpts{Retries: 3}
	ro := &flags.CliRootOpts{}

	callCount := 0
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		return fmt.Errorf("always fails")
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error to wrap context.Canceled, got: %v", err)
	}
	if elapsed >= time.Second {
		t.Fatalf("expected Operation to return promptly after cancellation, took %v", elapsed)
	}
	if callCount == 0 {
		t.Fatal("expected at least one attempt before cancellation")
	}
}

func TestOperation_AlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(testContext())
	cancel()

	rso := &flags.StoreRootOpts{Retries: 3}
	ro := &flags.CliRootOpts{}

	callCount := 0
	err := Operation(ctx, rso, ro, func() error {
		callCount++
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error to wrap context.Canceled, got: %v", err)
	}
	if callCount != 0 {
		t.Fatalf("expected operation to never be called, got %d calls", callCount)
	}
}

func TestOperation_WrapsLastError(t *testing.T) {
	ctx := testContext()
	rso := &flags.StoreRootOpts{Retries: 1}
	ro := &flags.CliRootOpts{}

	sentinel := errors.New("sentinel: 404 not found")

	err := Operation(ctx, rso, ro, func() error {
		return sentinel
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected error to wrap sentinel via errors.Is, got: %v", err)
	}
	if !strings.Contains(err.Error(), "sentinel: 404 not found") {
		t.Fatalf("expected error message to contain sentinel text, got: %q", err.Error())
	}
}
