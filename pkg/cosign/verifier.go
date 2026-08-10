package cosign

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"sync"

	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	cosignpkg "github.com/sigstore/cosign/v3/pkg/cosign"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/retry"
)

// defaultMaxWorkers bounds cosign's own per-image signature fan-out. Hauler
// already runs one Verify per image goroutine, so this only governs work
// within a single image's signature set.
const defaultMaxWorkers = 10

// Config is the fully-resolved verification input for one image. It is
// deliberately a comparable struct: Cache keys on it directly, so every image
// in a sync sharing a key -- 880 of them, in the Rancher case -- shares one
// Verifier and therefore one trust-material setup instead of 880.
type Config struct {
	Key  string
	Tlog bool

	CertIdentity                 string
	CertIdentityRegexp           string
	CertOidcIssuer               string
	CertOidcIssuerRegexp         string
	CertGithubWorkflowRepository string

	// TLS options for reaching the registry (signatures/attestations/SBOMs)
	// and the transparency log. InsecureSkipTLSVerify takes precedence over
	// CaFile -- see NewVerifier.
	InsecureSkipTLSVerify bool
	CaFile                string
}

// Empty reports whether cfg requests no verification at all.
func (c Config) Empty() bool { return c == Config{} }

// Keyless reports whether cfg verifies against a Fulcio identity rather than
// an explicit public key.
func (c Config) Keyless() bool { return c.Key == "" }

// validate rejects a key paired with any Cert* constraint, because supplying a
// key makes every one of them dead weight: cosign reads them only where
// co.SigVerifier is nil. verifyInternal (pkg/cosign/verify.go:871) skips the
// whole ValidateAndUnpackCertWithIntermediates -> CheckCertificatePolicy ->
// validateCertExtensions path once a verifier is set, and the only other
// reader, CheckOpts.verificationOptions, belongs to the NewBundleFormat path
// this package pins off. Accepting the combination would let a user who passed
// --certificate-identity believe they had pinned who signed the image when the
// signature was checked against the key alone. Cosign's own Exec rejects the
// narrower Key+CertIdentity case as KeyAndIdentityParseError
// (cmd/cosign/cli/verify/verify.go:102); the extra fields fail identically, so
// they are guarded identically.
func (c Config) validate() error {
	if c.Keyless() {
		return nil
	}
	for _, f := range []struct{ name, value string }{
		{"CertIdentity", c.CertIdentity},
		{"CertIdentityRegexp", c.CertIdentityRegexp},
		{"CertOidcIssuer", c.CertOidcIssuer},
		{"CertOidcIssuerRegexp", c.CertOidcIssuerRegexp},
		{"CertGithubWorkflowRepository", c.CertGithubWorkflowRepository},
	} {
		if f.value != "" {
			return fmt.Errorf("%s is set alongside a verification key; identity constraints apply only to keyless verification and would be ignored", f.name)
		}
	}
	return nil
}

// Verifier verifies images against one Config. Both CheckOpts are built once
// and then treated as read-only, which is what makes concurrent Verify calls
// safe: sigstore's package-level TUF and Fulcio state is touched only by the
// setup helpers below, all of which run before the Verifier is published.
type Verifier struct {
	// co drives classic tag-based verification; coBundle is its shallow copy
	// with NewBundleFormat set -- see NewVerifier for why they cannot be one.
	co       *cosignpkg.CheckOpts
	coBundle *cosignpkg.CheckOpts
	closeSV  func()

	// Retry settings, applied once per verification path by withRetry.
	rso *flags.StoreRootOpts
	ro  *flags.CliRootOpts
}

// NewVerifier builds the CheckOpts for cfg. It replicates the setup sequence in
// cosign v3.1.2's verify.VerifyCommand.Exec (cmd/cosign/cli/verify/verify.go:95-190)
// -- hauler cannot call Exec here because Exec prints, which forces the
// process-global output capture that serializes verification.
//
// ctx governs setup only in appearance: options.RegistryOptions.ClientOpts bakes
// it into co.RegistryClientOpts, which every later Verify reuses, so ctx also
// governs all registry I/O for the returned Verifier's whole lifetime. Pass a
// run-scoped ctx. A per-image or per-timeout ctx here cancels registry reads for
// every image sharing this Verifier the moment that one image's deadline fires.
func NewVerifier(ctx context.Context, cfg Config, rso *flags.StoreRootOpts, ro *flags.CliRootOpts) (*Verifier, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	var identities []cosignpkg.Identity
	if cfg.Keyless() {
		certOpts := options.CertVerifyOptions{
			CertOidcIssuer:               cfg.CertOidcIssuer,
			CertOidcIssuerRegexp:         cfg.CertOidcIssuerRegexp,
			CertIdentity:                 cfg.CertIdentity,
			CertIdentityRegexp:           cfg.CertIdentityRegexp,
			CertGithubWorkflowRepository: cfg.CertGithubWorkflowRepository,
		}
		var err error
		if identities, err = certOpts.Identities(); err != nil {
			return nil, fmt.Errorf("building identities: %w", err)
		}
	}

	// insecureSkipTLSVerify takes precedence: when set, caFile is ignored --
	// mirrors content.BuildTransport's precedence for the plain registry pull.
	regOpts := options.RegistryOptions{}
	if cfg.InsecureSkipTLSVerify {
		regOpts.AllowInsecure = true
	} else {
		regOpts.RegistryCACert = cfg.CaFile
	}
	ociremoteOpts, err := regOpts.ClientOpts(ctx)
	if err != nil {
		return nil, fmt.Errorf("constructing registry client options: %w", err)
	}

	// Keyless Fulcio certs expire ~10 minutes after issue, so the transparency
	// log is mandatory there to prove the cert was valid at signing time.
	// Keyed verification honors the caller's --tlog choice.
	ignoreTlog := !cfg.Tlog
	if cfg.Keyless() {
		ignoreTlog = false
	}

	co := &cosignpkg.CheckOpts{
		RegistryClientOpts:           ociremoteOpts,
		Identities:                   identities,
		CertGithubWorkflowRepository: cfg.CertGithubWorkflowRepository,
		IgnoreTlog:                   ignoreTlog,
		MaxWorkers:                   defaultMaxWorkers,
		// Must stay false: VerifyImageSignatures rejects a true value outright
		// with "bundle support for image signatures is not yet implemented"
		// (pkg/cosign/verify.go:645).
		NewBundleFormat: false,
	}

	// Mirrors cosign's unexported verifyOfflineWithKey (cli/verify/common.go:413):
	// no trusted root is needed when a key is supplied and neither Rekor nor
	// signed timestamps are consulted.
	offlineWithKey := !cfg.Keyless() && co.IgnoreTlog && !co.UseSignedTimestamps

	// Order is load-bearing and copied from Exec: trust material first, then
	// legacy clients, then the verifier -- LoadVerifierFromKeyOrCert validates
	// a certificate chain against the trust material and must see it populated.
	if err := verify.SetTrustedMaterial(ctx, "", "", "", "", "", offlineWithKey, co); err != nil {
		return nil, fmt.Errorf("setting trusted material: %w", err)
	}

	// The second and third arguments mirror cosign's unexported shouldVerifySCT
	// and keylessVerification (cli/verify/common.go:387,397); both reduce to
	// "no explicit key" given hauler never sets IgnoreSCT or a security key.
	if err := verify.SetLegacyClientsAndKeys(ctx, co.IgnoreTlog, cfg.Keyless(), cfg.Keyless(), "", "", "", "", "", co); err != nil {
		return nil, fmt.Errorf("setting up clients and keys: %w", err)
	}

	sv, _, closeSV, err := verify.LoadVerifierFromKeyOrCert(ctx, cfg.Key, "", "", "", crypto.SHA256, false, false, co)
	if err != nil {
		return nil, fmt.Errorf("loading verifier from key opts: %w", err)
	}
	co.SigVerifier = sv

	// VerifyImageAttestations reads co.NewBundleFormat directly, and co's copy
	// must stay false, so the bundle path needs a CheckOpts of its own. A
	// shallow copy rather than a second setup run: repeating the sequence above
	// would re-read the key file and, wherever offlineWithKey is false, refetch
	// TUF metadata, for a path most runs never take. Sharing RegistryClientOpts,
	// SigVerifier and TrustedMaterial by pointer is safe for the same reason one
	// CheckOpts can serve concurrent Verify calls -- cosign's verification paths
	// only read the CheckOpts they are handed; the one write on the bundle side
	// lands on a copy VerifyNewBundle makes first (pkg/cosign/verify_bundle.go:30).
	coBundle := *co
	coBundle.NewBundleFormat = true

	return &Verifier{co: co, coBundle: &coBundle, closeSV: closeSV, rso: rso, ro: ro}, nil
}

// Verify checks ref against v's config. ref should be a digest reference so the
// bytes verified are the bytes the caller goes on to store.
//
// The classic path handles tag-based signatures, which is every image in the
// workloads hauler targets. An image signed with the new sigstore bundle format
// has no .sig tag and so fails there with a not-found error; only that specific
// failure falls back to the bundle path.
func (v *Verifier) Verify(ctx context.Context, ref string) error {
	r, err := gname.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parsing reference %q: %w", ref, err)
	}

	if err = v.verifyImage(ctx, r); err == nil {
		return nil
	}
	if !fallbackEligible(err) {
		return err
	}
	return v.verifyBundle(ctx, r)
}

// verifyImage checks ref for a classic tag-based signature.
func (v *Verifier) verifyImage(ctx context.Context, r gname.Reference) error {
	return v.withRetry(ctx, func() error {
		_, _, err := cosignpkg.VerifyImageSignatures(ctx, r, v.co)
		return err
	})
}

// verifyBundle checks ref for a new-format sigstore bundle. In that format the
// signature is carried as an attestation, so VerifyImageAttestations is the
// entry point -- VerifyImageSignatures rejects NewBundleFormat outright
// (pkg/cosign/verify.go:645), and cosign's own Exec dispatches bundles the same
// way (cmd/cosign/cli/verify/verify.go:234). It returns its result rather than
// printing the verification report Exec prints, so unlike the CLI this path
// needs no log.CaptureOutput and does not serialize on its process-global
// mutex. The registry path it actually reaches --
// verifyImageAttestationsSigstoreBundle -> GetBundles + VerifyNewBundle
// (verify.go:1905, 1713; verify_bundle.go:25) -- has cosign write no
// diagnostics of its own: the ui.Warnf calls near this code (verify.go:1795-
// 1806) belong to GetLocalBundles, the local-OCI-layout sibling this function
// never calls.
//
// Reached only for images with no classic signature, so it stays off the hot
// path for the workloads hauler actually syncs.
func (v *Verifier) verifyBundle(ctx context.Context, r gname.Reference) error {
	return v.withRetry(ctx, func() error {
		_, _, err := cosignpkg.VerifyImageAttestations(ctx, r, v.coBundle)
		return err
	})
}

// withRetry runs one verification call under the caller's --retries budget.
//
// The budget is applied per path rather than around Verify as a whole: a loop
// outside Verify would re-run the classic attempt on its way back to the bundle
// path, costing retries*retries attempts for every bundle-signed image.
//
// Only errors another attempt could plausibly clear consume a retry --
// retryableVerifyErr decides. A terminal error leaves the loop by returning nil
// from the operation and travelling out through the captured variable, because
// retry.Operation has no other way to be told "stop, and this is not an
// exhausted budget".
func (v *Verifier) withRetry(ctx context.Context, call func() error) error {
	var terminal error
	exhausted := retry.Operation(ctx, v.rso, v.ro, func() error {
		err := call()
		if err != nil && !retryableVerifyErr(err) {
			terminal = err
			return nil
		}
		return err
	})
	if terminal != nil {
		return terminal
	}
	return exhausted
}

// retryableVerifyErr reports whether a further attempt at err could succeed for
// some reason other than luck.
//
// ErrNoMatchingSignatures is excluded for exactly the reason fallbackEligible
// excludes it: signatures were found and none validated, so every extra attempt
// is another chance for a bad signature to pass. Leaving it retryable would
// reintroduce that hazard by way of --retries.
//
// ErrNoMatchingAttestations is the bundle path's terminal answer, and cosign
// spells two conditions with it: every bundle found failed verification
// (verify.go:1986), which carries the same hazard as ErrNoMatchingSignatures,
// and no bundle was found at all (verify.go:1756). The type does not
// distinguish them, and GetBundles reaches the second by skipping past
// per-referrer fetch failures (verify.go:1746-1751), so a transient blip can
// arrive here spelled as absence. Excluding it reads that ambiguity the failing
// way: the cost is an image a further attempt might have verified, against a
// failed bundle drawing --retries more chances to pass.
//
// The fallbackEligible errors are excluded because the classic signature is
// absent and will stay absent; Verify hands them to verifyBundle, which draws
// its own full budget from withRetry.
func retryableVerifyErr(err error) bool {
	var noMatching *cosignpkg.ErrNoMatchingSignatures
	var noAtts *cosignpkg.ErrNoMatchingAttestations
	return !errors.As(err, &noMatching) && !errors.As(err, &noAtts) && !fallbackEligible(err)
}

// fallbackEligible reports whether err means "this image carries no classic
// tag-based signature" -- the only condition under which retrying down the
// bundle path is sound.
//
// ErrNoMatchingSignatures is deliberately excluded. It means signatures were
// found and none of them validated; retrying that would give an image with a
// bad signature a second chance to pass, which is the one outcome this design
// must never permit. Every other error -- transport failures included -- also
// fails closed rather than falling back.
func fallbackEligible(err error) bool {
	var tagNotFound *cosignpkg.ErrImageTagNotFound
	var noSigs *cosignpkg.ErrNoSignaturesFound
	return errors.As(err, &tagNotFound) || errors.As(err, &noSigs)
}

// Close releases the signature verifier's resources. It must not be called
// until every in-flight Verify has returned.
func (v *Verifier) Close() {
	if v.closeSV != nil {
		v.closeSV()
	}
}

// Cache hands out one Verifier per distinct Config for the lifetime of a run.
// The retry options live here rather than in Get's arguments because only
// Config keys the map: a per-call value would be silently ignored for every
// image after the first that shares a Config.
//
// With several distinct configs, building one Verifier can overlap another
// Verifier's in-flight Verify calls. That is safe for the reason Verifier's doc
// gives, plus sigstore v1.10.8's pkg/tuf guarding its own singleton (sync.Once
// plus initMu, client.go:61-68).
type Cache struct {
	mu  sync.Mutex
	m   map[Config]*cacheEntry
	rso *flags.StoreRootOpts
	ro  *flags.CliRootOpts
}

type cacheEntry struct {
	v   *Verifier
	err error
}

// NewCache returns an empty Cache. rso and ro carry the --retries/--ignore-errors
// settings every Verifier it builds applies to verification.
func NewCache(rso *flags.StoreRootOpts, ro *flags.CliRootOpts) *Cache {
	return &Cache{m: make(map[Config]*cacheEntry), rso: rso, ro: ro}
}

// Get returns the Verifier for cfg, building it on first request. A build
// failure is cached too, so a bad key path fails fast for every image that
// shares it instead of re-reading and re-failing 880 times.
//
// ctx must be run-scoped, never per-image. Only the caller that loses no race
// -- whichever one finds cfg cold -- has its ctx handed to NewVerifier, and
// that ctx then lives inside the shared registry options every subsequent
// caller's Verify uses (see NewVerifier's doc). Passing a per-image timeout ctx
// therefore cancels registry I/O for all images sharing cfg when that one
// image's deadline fires, and which image that is depends on scheduling.
//
// The lock is held across NewVerifier rather than released around it: the whole
// point of the cache is that sigstore's trust-material setup runs once, and a
// double-checked scheme would let two goroutines both run it on a cold key.
func (c *Cache) Get(ctx context.Context, cfg Config) (*Verifier, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.m[cfg]; ok {
		return e.v, e.err
	}
	v, err := NewVerifier(ctx, cfg, c.rso, c.ro)
	c.m[cfg] = &cacheEntry{v: v, err: err}
	return v, err
}

// Close releases every Verifier built by this Cache. It must not be called
// until every in-flight Verify has returned.
func (c *Cache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.m {
		if e.v != nil {
			e.v.Close()
		}
	}
}
