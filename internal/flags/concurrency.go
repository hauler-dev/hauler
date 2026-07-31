package flags

import (
	"fmt"
	"os"
	"strconv"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// ResolveConcurrency returns the effective --concurrency value for `store
// sync`, honoring explicit-flag > HAULER_CONCURRENCY env var > default
// precedence. flagChanged is cmd.Flags().Changed("concurrency"); flagValue
// is o.Concurrency as bound by cobra (already consts.DefaultConcurrency
// when the user didn't pass the flag). Values < 1 are rejected outright,
// never silently clamped to 1 -- clamping would hide a typo'd
// --concurrency 0 or a bad env var behind "it just worked".
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

// BlobConcurrencyFor derives the OCI layout's blob-write concurrency ceiling
// for a given resolved --concurrency value: max(16, 4*concurrency), capped
// at 32. The floor of consts.DefaultBlobConcurrency (16) matters and must
// stay commented: layer writes within a single image are bounded only by
// this shared semaphore (content.OCI.blobSem), not by --concurrency, so a
// naive 4*concurrency would make --concurrency 1 slower than today's
// behavior on any image with more than 4 layers. The 32 cap keeps a
// pathologically wide image or a large --concurrency from opening unbounded
// sockets.
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
