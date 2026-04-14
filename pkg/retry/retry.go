package retry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
)

// Operation retries the given operation according to the retry settings in rso/ro.
func Operation(ctx context.Context, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, operation func() error) error {
	l := log.FromContext(ctx)

	if !ro.IgnoreErrors {
		if os.Getenv(consts.HaulerIgnoreErrors) == "true" {
			ro.IgnoreErrors = true
		}
	}

	retries := rso.Retries
	if retries <= 0 {
		retries = consts.DefaultRetries
	}

	for attempt := 1; attempt <= retries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		isTlogErr := strings.HasPrefix(err.Error(), "function execution failed: no matching signatures: rekor client not provided for online verification")
		if ro.IgnoreErrors {
			if isTlogErr {
				l.Warnf("warning (attempt %d/%d)... failed tlog verification", attempt, retries)
			} else {
				l.Warnf("warning (attempt %d/%d)... %v", attempt, retries, err)
			}
		} else {
			if isTlogErr {
				l.Errorf("error (attempt %d/%d)... failed tlog verification", attempt, retries)
			} else {
				l.Errorf("error (attempt %d/%d)... %v", attempt, retries, err)
			}
		}

		if attempt < retries {
			time.Sleep(time.Second * consts.RetriesInterval)
		}
	}

	return fmt.Errorf("operation unsuccessful after %d attempts", retries)
}
