package cosign

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sigstore/cosign/v2/cmd/cosign/cli"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/verify"
	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/artifacts/image"
	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/store"
	"oras.land/oras-go/pkg/content"
)

// VerifyFileSignature verifies the digital signature of a file using Sigstore/Cosign.
func VerifySignature(ctx context.Context, s *store.Layout, keyPath string, useTlog bool, ref string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	operation := func() error {
		v := &verify.VerifyCommand{
			KeyRef:     keyPath,
			IgnoreTlog: true, // Ignore transparency log by default.
		}

		// if the user wants to use the transparency log, set the flag to false
		if useTlog {
			v.IgnoreTlog = false
		}

		err := log.CaptureOutput(l, true, func() error {
			return v.Exec(ctx, []string{ref})
		})
		if err != nil {
			return err
		}

		return nil
	}

	return RetryOperation(ctx, rso, ro, operation)
}

// SaveImage saves image and any signatures/attestations to the store.
func SaveImage(ctx context.Context, s *store.Layout, ref string, platform string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	if !ro.IgnoreErrors {
		envVar := os.Getenv(consts.HaulerIgnoreErrors)
		if envVar == "true" {
			ro.IgnoreErrors = true
		}
	}

	operation := func() error {
		o := &options.SaveOptions{
			Directory: s.Root,
		}

		// check to see if the image is multi-arch
		isMultiArch, err := image.IsMultiArchImage(ref)
		if err != nil {
			return err
		}
		l.Debugf("multi-arch image [%v]", isMultiArch)

		// Conditionally add platform.
		if platform != "" && isMultiArch {
			l.Debugf("platform for image [%s]", platform)
			o.Platform = platform
		}

		err = cli.SaveCmd(ctx, *o, ref)
		if err != nil {
			return err
		}

		return nil

	}

	return RetryOperation(ctx, rso, ro, operation)
}

// LoadImage loads store to a remote registry.
func LoadImages(ctx context.Context, s *store.Layout, registry string, ropts content.RegistryOptions, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)

	o := &options.LoadOptions{
		Directory: s.Root,
		Registry: options.RegistryOptions{
			Name: registry,
		},
	}

	// Conditionally add extra registry flags.
	if ropts.Insecure {
		o.Registry.AllowInsecure = true
	}
	if ropts.PlainHTTP {
		o.Registry.AllowHTTPRegistry = true
	}

	if ropts.Username != "" {
		o.Registry.AuthConfig.Username = ropts.Username
		o.Registry.AuthConfig.Password = ropts.Password
	}

	// execute the cosign load and capture the output in our logger
	err := log.CaptureOutput(l, false, func() error {
		return cli.LoadCmd(ctx, *o, "")
	})
	if err != nil {
		return err
	}

	return nil
}

func RetryOperation(ctx context.Context, rso *flags.StoreRootOpts, ro *flags.CliRootOpts, operation func() error) error {
	l := log.FromContext(ctx)

	if !ro.IgnoreErrors {
		envVar := os.Getenv(consts.HaulerIgnoreErrors)
		if envVar == "true" {
			ro.IgnoreErrors = true
		}
	}

	// Validate retries and fall back to a default
	retries := rso.Retries
	if retries <= 0 {
		retries = consts.DefaultRetries
	}

	for attempt := 1; attempt <= rso.Retries; attempt++ {
		err := operation()
		if err == nil {
			// If the operation succeeds, return nil (no error)
			return nil
		}

		if ro.IgnoreErrors {
			if strings.HasPrefix(err.Error(), "function execution failed: no matching signatures: rekor client not provided for online verification") {
				l.Warnf("warning (attempt %d/%d)... failed tlog verification", attempt, rso.Retries)
			} else {
				l.Warnf("warning (attempt %d/%d)... %v", attempt, rso.Retries, err)
			}
		} else {
			if strings.HasPrefix(err.Error(), "function execution failed: no matching signatures: rekor client not provided for online verification") {
				l.Errorf("error (attempt %d/%d)... failed tlog verification", attempt, rso.Retries)
			} else {
				l.Errorf("error (attempt %d/%d)... %v", attempt, rso.Retries, err)
			}
		}

		// If this is not the last attempt, wait before retrying
		if attempt < rso.Retries {
			time.Sleep(time.Second * consts.RetriesInterval)
		}
	}

	// If all attempts fail, return an error
	return fmt.Errorf("operation unsuccessful after %d attempts", rso.Retries)
}
