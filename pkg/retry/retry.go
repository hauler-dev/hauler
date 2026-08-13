package retry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// Operation retries the given operation according to the retry settings in rso/ro.
func Operation(ctx context.Context, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, operation func() error) error {
	l := log.FromContext(ctx)

	ignoreErrors := flags.ShouldIgnoreErrors(ro)

	retries := rso.Retries
	if retries <= 0 {
		retries = consts.DefaultRetries
	}

	var lastErr error

	for attempt := 1; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := operation()
		if err == nil {
			return nil
		}
		lastErr = err

		isTlogErr := strings.HasPrefix(err.Error(), "function execution failed: no matching signatures: rekor client not provided for online verification")
		if ignoreErrors {
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
			timer := time.NewTimer(time.Second * consts.RetriesInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}

	return fmt.Errorf("operation unsuccessful after %d attempts: %w", retries, lastErr)
}
