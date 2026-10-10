package store_test

import (
	"os"
	"path/filepath"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/flags"
	"hauler.dev/go/hauler/v2/pkg/store"
)

// The Store()-behaviour tests below were relocated from pkg/flags when
// (*StoreRootOpts).Store moved to pkg/store.Open (a method cannot live on a
// type in another package). They pin the same behaviours: env resolution,
// explicit-value precedence, negative-value rejection, and the receiver
// mutation callers depend on.

// TestOpenResolvesBlobConcurrencyFromEnv verifies that a subcommand which
// never runs sync's PreRunE -- `store add`, `store load`, `store copy` --
// still picks up HAULER_BLOB_CONCURRENCY.
func TestOpenResolvesBlobConcurrencyFromEnv(t *testing.T) {
	t.Setenv(consts.HaulerBlobConcurrency, "3")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.BlobConcurrency != 3 {
		t.Fatalf("got BlobConcurrency %d, want 3", o.BlobConcurrency)
	}
}

// TestOpenLeavesExplicitBlobConcurrencyAlone verifies Open() does not
// overwrite a value already resolved by sync's PreRunE.
func TestOpenLeavesExplicitBlobConcurrencyAlone(t *testing.T) {
	t.Setenv(consts.HaulerBlobConcurrency, "3")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{BlobConcurrency: 20}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.BlobConcurrency != 20 {
		t.Fatalf("got BlobConcurrency %d, want 20 (env must not override)", o.BlobConcurrency)
	}
}

// TestOpenRejectsNegativeBlobConcurrency verifies that a negative
// --blob-concurrency passed to a subcommand with no PreRunE of its own (add,
// load, copy, serve, extract) surfaces an error instead of being silently
// dropped.
func TestOpenRejectsNegativeBlobConcurrency(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{BlobConcurrency: -5}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err == nil {
		t.Fatal("expected an error for negative --blob-concurrency, got nil")
	}
}

// TestOpenResolvesRetriesFromEnv verifies that a subcommand which never
// touches --retries directly -- `store add`, `store load`, `store copy` --
// still picks up HAULER_RETRIES.
func TestOpenResolvesRetriesFromEnv(t *testing.T) {
	t.Setenv(consts.HaulerRetries, "7")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Retries != 7 {
		t.Fatalf("got Retries %d, want 7", o.Retries)
	}
}

// TestOpenLeavesExplicitRetriesAlone verifies Open() does not overwrite an
// explicit --retries with HAULER_RETRIES.
func TestOpenLeavesExplicitRetriesAlone(t *testing.T) {
	t.Setenv(consts.HaulerRetries, "7")
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{Retries: 2}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Retries != 2 {
		t.Fatalf("got Retries %d, want 2 (env must not override)", o.Retries)
	}
}

// TestOpenDefaultsRetriesWhenUnset verifies Open() falls back to
// consts.DefaultRetries when neither --retries nor HAULER_RETRIES is set.
func TestOpenDefaultsRetriesWhenUnset(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.Retries != consts.DefaultRetries {
		t.Fatalf("got Retries %d, want default %d", o.Retries, consts.DefaultRetries)
	}
}

// TestOpenRejectsNegativeRetries verifies that a negative --retries surfaces
// an error instead of being silently dropped, same as --blob-concurrency.
func TestOpenRejectsNegativeRetries(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	o := &flags.StoreRootOpts{Retries: -5}
	if _, err := store.Open(t.Context(), o, &flags.CliRootOpts{}); err == nil {
		t.Fatal("expected an error for negative --retries, got nil")
	}
}

// TestResolveStoreDir_EnvFallbackIsAbsolute pins the acceptance behaviour:
// when storeDir is empty and HAULER_STORE_DIR is set, ResolveStoreDir returns
// that env path, made absolute.
func TestResolveStoreDir_EnvFallbackIsAbsolute(t *testing.T) {
	envDir := t.TempDir()
	t.Setenv(consts.HaulerStoreDir, envDir)
	t.Setenv(consts.HaulerDir, t.TempDir())

	got, err := store.ResolveStoreDir(t.Context(), &flags.CliRootOpts{}, "")
	if err != nil {
		t.Fatalf("ResolveStoreDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ResolveStoreDir returned non-absolute path %q", got)
	}
	want, err := filepath.Abs(envDir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveStoreDir() = %q, want env path %q", got, want)
	}
}

// TestResolveStoreDir_RegisteredStoreID pins the acceptance behaviour: a
// StoreID registered by a previous NewLayout(WithHaulerDir(...)) resolves to
// the store's recorded path.
func TestResolveStoreDir_RegisteredStoreID(t *testing.T) {
	haulerDir := t.TempDir()
	storeDir := t.TempDir()

	s, err := store.NewLayout(storeDir, store.WithHaulerDir(haulerDir))
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	if s.StoreID == "" {
		t.Fatal("NewLayout returned an empty StoreID")
	}

	ro := &flags.CliRootOpts{HaulerDir: haulerDir}
	got, err := store.ResolveStoreDir(t.Context(), ro, s.StoreID)
	if err != nil {
		t.Fatalf("ResolveStoreDir(%q): %v", s.StoreID, err)
	}
	want, err := filepath.Abs(storeDir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if got != want {
		t.Fatalf("ResolveStoreDir() = %q, want registered path %q", got, want)
	}
}

// TestResolveStoreDir_StaleRegistrationErrors pins the acceptance behaviour:
// when a registered StoreID's recorded path no longer holds that store, the
// lookup errors rather than silently returning the stale path.
func TestResolveStoreDir_StaleRegistrationErrors(t *testing.T) {
	haulerDir := t.TempDir()
	storeDir := t.TempDir()

	s, err := store.NewLayout(storeDir, store.WithHaulerDir(haulerDir))
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}

	// Make the registration stale: remove the store's identity metadata so
	// MatchesStoreID no longer recognizes the recorded path.
	if err := os.Remove(filepath.Join(storeDir, consts.DefaultStoreMetadataName)); err != nil {
		t.Fatalf("removing store metadata: %v", err)
	}

	ro := &flags.CliRootOpts{HaulerDir: haulerDir}
	if _, err := store.ResolveStoreDir(t.Context(), ro, s.StoreID); err == nil {
		t.Fatal("expected an error for a stale StoreID registration, got nil")
	}
}

// TestOpen_RootsLayoutAtResolvedDir pins the acceptance behaviour: Open
// returns a *Layout rooted at the resolved store dir and honors WithHaulerDir
// (the store is registered in the inventory).
func TestOpen_RootsLayoutAtResolvedDir(t *testing.T) {
	haulerDir := t.TempDir()
	storeDir := t.TempDir()
	t.Setenv(consts.HaulerStoreDir, storeDir)

	ro := &flags.CliRootOpts{HaulerDir: haulerDir}
	rso := &flags.StoreRootOpts{}
	s, err := store.Open(t.Context(), rso, ro)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want, err := filepath.Abs(storeDir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if s.Root != want {
		t.Fatalf("Layout.Root = %q, want resolved dir %q", s.Root, want)
	}
	// Open must preserve the receiver-mutation side effect callers rely on.
	if rso.StoreDir != want {
		t.Fatalf("rso.StoreDir = %q, want %q (receiver mutation lost)", rso.StoreDir, want)
	}
	// WithHaulerDir must have registered the store in the inventory.
	if _, err := os.Stat(filepath.Join(haulerDir, consts.DefaultStoreInventoryName)); err != nil {
		t.Fatalf("expected store inventory at %s: %v", haulerDir, err)
	}
}

// TestOpen_HonorsBlobConcurrency pins the acceptance behaviour: Open passes a
// resolved blob-concurrency ceiling through to the Layout (WithBlobConcurrency).
func TestOpen_HonorsBlobConcurrency(t *testing.T) {
	t.Setenv(consts.HaulerStoreDir, t.TempDir())
	t.Setenv(consts.HaulerDir, t.TempDir())

	rso := &flags.StoreRootOpts{BlobConcurrency: 5}
	if _, err := store.Open(t.Context(), rso, &flags.CliRootOpts{}); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if rso.BlobConcurrency != 5 {
		t.Fatalf("rso.BlobConcurrency = %d, want 5 (WithBlobConcurrency not honored)", rso.BlobConcurrency)
	}
}
