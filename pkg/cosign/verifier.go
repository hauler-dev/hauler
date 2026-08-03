package cosign

import (
	"context"
	"crypto"
	"fmt"
	"sync"

	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v3/cmd/cosign/cli/verify"
	cosignpkg "github.com/sigstore/cosign/v3/pkg/cosign"
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

// Verifier verifies images against one Config. Its CheckOpts is built once and
// then treated as read-only, which is what makes concurrent Verify calls safe:
// sigstore's package-level TUF and Fulcio state is touched only by the setup
// helpers below, all of which run before the Verifier is published.
type Verifier struct {
	cfg     Config
	co      *cosignpkg.CheckOpts
	closeSV func()
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
func NewVerifier(ctx context.Context, cfg Config) (*Verifier, error) {
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

	regOpts := options.RegistryOptions{}
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

	return &Verifier{cfg: cfg, co: co, closeSV: closeSV}, nil
}

// Verify checks ref against v's config. ref should be a digest reference so the
// bytes verified are the bytes the caller goes on to store.
func (v *Verifier) Verify(ctx context.Context, ref string) error {
	r, err := gname.ParseReference(ref)
	if err != nil {
		return fmt.Errorf("parsing reference %q: %w", ref, err)
	}
	_, _, err = cosignpkg.VerifyImageSignatures(ctx, r, v.co)
	return err
}

// Close releases the signature verifier's resources. It must not be called
// until every in-flight Verify has returned.
func (v *Verifier) Close() {
	if v.closeSV != nil {
		v.closeSV()
	}
}

// Cache hands out one Verifier per distinct Config for the lifetime of a run.
type Cache struct {
	mu sync.Mutex
	m  map[Config]*cacheEntry
}

type cacheEntry struct {
	v   *Verifier
	err error
}

// NewCache returns an empty Cache.
func NewCache() *Cache {
	return &Cache{m: make(map[Config]*cacheEntry)}
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
	v, err := NewVerifier(ctx, cfg)
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
