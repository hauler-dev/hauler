package flags

import (
	"fmt"
	"os"
	"strconv"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// ResolveConcurrency returns the effective --concurrency value for `store
// sync`, honoring explicit-flag > HAULER_CONCURRENCY env var > default
// precedence. Values < 1 are rejected outright, never clamped -- clamping
// would hide a typo'd --concurrency 0 or a bad env var behind "it just
// worked".
func ResolveConcurrency(flagChanged bool, flagValue int) (int, error) {
	if flagChanged {
		if flagValue < 1 {
			return 0, fmt.Errorf("--concurrency must be >= 1, got %d", flagValue)
		}
		return flagValue, nil
	}

	if v := os.Getenv(consts.HaulerConcurrency); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("invalid %s value %q: %w", consts.HaulerConcurrency, v, err)
		}
		if n < 1 {
			return 0, fmt.Errorf("%s must be >= 1, got %d", consts.HaulerConcurrency, n)
		}
		return n, nil
	}

	return consts.DefaultConcurrency, nil
}

// BlobConcurrencyFor derives the OCI layout's blob-write concurrency
// ceiling from a resolved --concurrency value: max(16, 4*concurrency),
// capped at 32. The floor of consts.DefaultBlobConcurrency (16) matters:
// layer writes within a single image are bounded only by the shared
// blobSem (content.OCI.blobSem), not by --concurrency, so a naive
// 4*concurrency would make --concurrency 1 slower than today's behavior on
// images with more than 4 layers. The cap keeps a pathologically wide image
// or large --concurrency from opening unbounded sockets.
func BlobConcurrencyFor(concurrency int) int {
	n := 4 * concurrency
	if n < consts.DefaultBlobConcurrency {
		n = consts.DefaultBlobConcurrency
	}
	if n > 32 {
		n = 32
	}
	return n
}

// ResolveBlobConcurrency returns an explicitly-requested blob-write
// concurrency ceiling, honoring flag > HAULER_BLOB_CONCURRENCY precedence.
// It returns 0 when neither was supplied ("not specified"); the caller
// picks the fallback (SyncBlobConcurrency derives one; every other store
// subcommand leaves it to consts.DefaultBlobConcurrency). Unlike
// ResolveConcurrency, 0 here is a deliberate "auto" sentinel rather than an
// error, letting an explicit value bypass BlobConcurrencyFor's floor and
// cap entirely -- the only way to measure disk fan-out on a low-IOPS
// volume, since the floor of 16 otherwise keeps --concurrency 1 at 16
// concurrent blob writes. Negative values and unparseable env values are
// still rejected outright, matching ResolveConcurrency's rule that a typo
// must surface rather than appear to work.
func ResolveBlobConcurrency(flagValue int) (int, error) {
	if flagValue < 0 {
		return 0, fmt.Errorf("--blob-concurrency must be >= 0, got %d", flagValue)
	}
	if flagValue > 0 {
		return flagValue, nil
	}

	v := os.Getenv(consts.HaulerBlobConcurrency)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", consts.HaulerBlobConcurrency, v, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s must be >= 1, got %d", consts.HaulerBlobConcurrency, n)
	}
	return n, nil
}

// SyncBlobConcurrency resolves the effective blob-write ceiling for `store
// sync`: an explicit --blob-concurrency (or HAULER_BLOB_CONCURRENCY) value
// wins outright, otherwise the value is derived from the already-resolved
// --concurrency via BlobConcurrencyFor. It never returns 0.
func SyncBlobConcurrency(blobFlagValue, concurrency int) (int, error) {
	bc, err := ResolveBlobConcurrency(blobFlagValue)
	if err != nil {
		return 0, err
	}
	if bc == 0 {
		bc = BlobConcurrencyFor(concurrency)
	}
	return bc, nil
}
