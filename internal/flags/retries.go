package flags

import (
	"fmt"
	"os"
	"strconv"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

// ResolveRetries resolves flag > HAULER_RETRIES > default, same shape as
// ResolveBlobConcurrency. 0 means "use the default", not "never retry".
func ResolveRetries(flagValue int) (int, error) {
	if flagValue < 0 {
		return 0, fmt.Errorf("--retries must be >= 0, got %d", flagValue)
	}
	if flagValue > 0 {
		return flagValue, nil
	}

	v := os.Getenv(consts.HaulerRetries)
	if v == "" {
		return consts.DefaultRetries, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", consts.HaulerRetries, v, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s must be >= 0, got %d", consts.HaulerRetries, n)
	}
	if n == 0 {
		return consts.DefaultRetries, nil
	}
	return n, nil
}
