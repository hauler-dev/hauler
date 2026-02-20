package cosign

import (
	"context"

	"github.com/sigstore/cosign/v3/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	"hauler.dev/go/hauler/internal/flags"
	"hauler.dev/go/hauler/pkg/log"
	"hauler.dev/go/hauler/pkg/retry"
)

// VerifySignature verifies the digital signature of an image using Sigstore/Cosign.
func VerifySignature(ctx context.Context, keyPath string, useTlog bool, ref string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	operation := func() error {
		v := &verify.VerifyCommand{
			KeyRef:          keyPath,
			IgnoreTlog:      true, // Ignore transparency log by default.
			NewBundleFormat: true,
		}

		if useTlog {
			v.IgnoreTlog = false
		}

		return log.CaptureOutput(l, true, func() error {
			return v.Exec(ctx, []string{ref})
		})
	}
	return retry.Operation(ctx, rso, ro, operation)
}

// VerifyKeylessSignature verifies an image signature using keyless/OIDC identity.
func VerifyKeylessSignature(ctx context.Context, identity string, identityRegexp string, oidcIssuer string, oidcIssuerRegexp string, ghWorkflowRepository string, useTlog bool, ref string, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) error {
	l := log.FromContext(ctx)
	operation := func() error {
		certVerifyOptions := options.CertVerifyOptions{
			CertOidcIssuer:               oidcIssuer,
			CertOidcIssuerRegexp:         oidcIssuerRegexp,
			CertIdentity:                 identity,
			CertIdentityRegexp:           identityRegexp,
			CertGithubWorkflowRepository: ghWorkflowRepository,
		}

		v := &verify.VerifyCommand{
			CertVerifyOptions:            certVerifyOptions,
			IgnoreTlog:                   false, // Use transparency log by default for keyless verification.
			CertGithubWorkflowRepository: ghWorkflowRepository,
			NewBundleFormat:              true,
		}

		if !useTlog {
			v.IgnoreTlog = true
		}

		return log.CaptureOutput(l, true, func() error {
			return v.Exec(ctx, []string{ref})
		})
	}
	return retry.Operation(ctx, rso, ro, operation)
}
