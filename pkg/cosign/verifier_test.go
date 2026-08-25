package cosign

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	golog "log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	goname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/rs/zerolog"
	cosignpkg "github.com/sigstore/cosign/v3/pkg/cosign"
	ociremote "github.com/sigstore/cosign/v3/pkg/oci/remote"

	"hauler.dev/go/hauler/v2/internal/flags"
	"hauler.dev/go/hauler/v2/pkg/reference"
)

// testOpts returns retry options for the constructors that plumb them to the
// fallback path. Retries must be non-zero: retry.Operation dereferences rso, so
// a nil would turn a test that wrongly falls back into a panic instead of a
// readable failure, and the default 3 would add two 5-second waits to it.
func testOpts() (*flags.StoreRootOpts, *flags.CliRootOpts) {
	return &flags.StoreRootOpts{Retries: 1}, &flags.CliRootOpts{}
}

// writeTestPubKey writes a fresh ECDSA P-256 public key in PEM form and returns
// its path. Generated rather than vendored so the test never depends on a
// fixture whose algorithm cosign might later stop accepting.
func writeTestPubKey(t *testing.T) string {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshaling public key: %v", err)
	}

	path := filepath.Join(t.TempDir(), "cosign.pub")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("writing public key: %v", err)
	}
	return path
}

func TestConfigPredicates(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantEmpty   bool
		wantKeyless bool
	}{
		{name: "zero value", cfg: Config{}, wantEmpty: true, wantKeyless: true},
		{name: "key only", cfg: Config{Key: "/tmp/cosign.pub"}, wantEmpty: false, wantKeyless: false},
		{name: "key with tlog", cfg: Config{Key: "/tmp/cosign.pub", Tlog: true}, wantEmpty: false, wantKeyless: false},
		{name: "identity only", cfg: Config{CertIdentity: "me@example.com"}, wantEmpty: false, wantKeyless: true},
		{name: "issuer regexp only", cfg: Config{CertOidcIssuerRegexp: ".*"}, wantEmpty: false, wantKeyless: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Empty(); got != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, tt.wantEmpty)
			}
			if got := tt.cfg.Keyless(); got != tt.wantKeyless {
				t.Errorf("Keyless() = %v, want %v", got, tt.wantKeyless)
			}
		})
	}
}

func TestCacheBuildsOneVerifierPerConfig(t *testing.T) {
	keyPath := writeTestPubKey(t)
	cfg := Config{Key: keyPath}

	c := NewCache(testOpts())
	defer c.Close()

	first, err := c.Get(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	second, err := c.Get(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if first != second {
		t.Fatal("Cache.Get returned distinct Verifiers for an identical Config; " +
			"trust material would be rebuilt per image")
	}
	if len(c.m) != 1 {
		t.Fatalf("two Gets on one Config produced %d cache entries, want 1", len(c.m))
	}

	other, err := c.Get(context.Background(), Config{Key: writeTestPubKey(t)})
	if err != nil {
		t.Fatalf("second-key Get: %v", err)
	}
	if other == first {
		t.Fatal("Cache.Get shared a Verifier across configs that differ in Key")
	}

	// Tlog: true alongside a key makes offlineWithKey false, so this build
	// reaches cosign.TrustedRoot() -- a TUF fetch (cli/verify/common.go:191).
	// Online it succeeds and SetLegacyClientsAndKeys then returns early at
	// common.go:140 on co.TrustedMaterial != nil; offline SetTrustedMaterial
	// only warns, leaving TrustedMaterial nil, and the build instead fails at
	// GetRekorPubs. Asserting on the entry count rather than on the returned
	// pointer holds either way: a failed build is still cached under its own
	// Config, so the count rises iff the cache key covers Tlog.
	_, _ = c.Get(context.Background(), Config{Key: keyPath, Tlog: true})
	if len(c.m) != 3 {
		t.Fatalf("a Config differing only in Tlog produced %d cache entries, want 3; the key is too coarse", len(c.m))
	}
}

func TestCacheGetIsConcurrencySafe(t *testing.T) {
	keyPath := writeTestPubKey(t)
	c := NewCache(testOpts())
	defer c.Close()

	var wg sync.WaitGroup
	got := make([]*Verifier, 16)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.Get(context.Background(), Config{Key: keyPath})
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			got[i] = v
		}()
	}
	wg.Wait()

	for i, v := range got {
		if v != got[0] {
			t.Fatalf("goroutine %d got a different Verifier; the cache raced", i)
		}
	}
}

// A build failure must be cached, or a bad key path is re-read and re-fails
// once per image in a sync.
func TestCacheCachesBuildFailure(t *testing.T) {
	c := NewCache(testOpts())
	defer c.Close()

	cfg := Config{Key: filepath.Join(t.TempDir(), "missing.pub")}

	first, firstErr := c.Get(context.Background(), cfg)
	if firstErr == nil {
		t.Fatal("Get succeeded for a nonexistent key path")
	}
	if first != nil {
		t.Fatal("Get returned a non-nil Verifier alongside an error")
	}

	second, secondErr := c.Get(context.Background(), cfg)
	if secondErr != firstErr {
		t.Fatalf("Get rebuilt after a failure: first %v, second %v", firstErr, secondErr)
	}
	if second != nil {
		t.Fatal("Get returned a non-nil Verifier alongside a cached error")
	}
}

// A keyless Config must name both a subject and an issuer; either alone leaves
// the other unconstrained. The assertions pin the "building identities" wrapper
// rather than just err != nil so that a reordering of NewVerifier's setup --
// which would make these fail for some unrelated reason instead -- is caught.
func TestKeylessVerificationRequiresFullIdentity(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "subject without issuer", cfg: Config{CertIdentity: "me@example.com"}},
		{name: "subject regexp without issuer", cfg: Config{CertIdentityRegexp: ".*@example.com"}},
		{name: "issuer without subject", cfg: Config{CertOidcIssuer: "https://accounts.example.com"}},
		{name: "issuer regexp without subject", cfg: Config{CertOidcIssuerRegexp: "https://.*"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rso, ro := testOpts()
			_, err := NewVerifier(context.Background(), tt.cfg, rso, ro)
			if err == nil {
				t.Fatal("NewVerifier accepted a keyless Config with a half-specified identity")
			}
			if !strings.Contains(err.Error(), "building identities") {
				t.Fatalf("failed somewhere other than identity construction: %v", err)
			}
		})
	}
}

// A key plus any cert constraint must not verify against the key alone: the
// user asked to pin who signed it, and cosign reads none of the Cert* fields
// once co.SigVerifier is set. One subtest per guarded field, so a copy-paste
// slip that drops one from validate's list fails on that field's name.
func TestKeyWithCertConstraintIsRejected(t *testing.T) {
	keyPath := writeTestPubKey(t)

	tests := []struct {
		name  string
		field string
		cfg   Config
	}{
		{name: "identity", field: "CertIdentity", cfg: Config{Key: keyPath, CertIdentity: "me@example.com"}},
		{name: "identity regexp", field: "CertIdentityRegexp", cfg: Config{Key: keyPath, CertIdentityRegexp: ".*"}},
		{name: "issuer", field: "CertOidcIssuer", cfg: Config{Key: keyPath, CertOidcIssuer: "https://accounts.example.com"}},
		{name: "issuer regexp", field: "CertOidcIssuerRegexp", cfg: Config{Key: keyPath, CertOidcIssuerRegexp: "https://.*"}},
		{name: "github workflow repository", field: "CertGithubWorkflowRepository", cfg: Config{Key: keyPath, CertGithubWorkflowRepository: "example/repo"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rso, ro := testOpts()
			v, err := NewVerifier(context.Background(), tt.cfg, rso, ro)
			if err == nil {
				t.Fatalf("NewVerifier built a Verifier that silently ignores %s", tt.field)
			}
			if v != nil {
				t.Fatal("NewVerifier returned a Verifier alongside an error")
			}
			if !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("error does not name the ignored field %s: %v", tt.field, err)
			}
		})
	}
}

// The guard must not be so wide that it rejects the configs hauler actually
// builds: sync's key branch leaves every Cert* field zero.
func TestKeyAloneIsAccepted(t *testing.T) {
	rso, ro := testOpts()
	if _, err := NewVerifier(context.Background(), Config{Key: writeTestPubKey(t)}, rso, ro); err != nil {
		t.Fatalf("NewVerifier rejected a plain keyed Config: %v", err)
	}
}

// newSigTestRegistry starts an in-process OCI registry and returns its host
// plus the remote options that reach it. Its request log is discarded: the
// verification failures under test each drive a dozen requests, and cosign's
// own probes make the noise larger than the assertions.
func newSigTestRegistry(t *testing.T) (string, []remote.Option) {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(golog.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://"), []remote.Option{remote.WithTransport(srv.Client().Transport)}
}

// seedSigTestImage pushes a random image to repo:latest. When withSigTag is
// set it also pushes a random image at the cosign v2 signature tag for that
// digest -- a manifest whose layers parse as signatures but carry no
// dev.cosignproject.cosign/signature annotation, so every one of them fails
// verification. That is the "found signatures, none valid" condition.
func seedSigTestImage(t *testing.T, host, repo string, withSigTag bool, ropts []remote.Option) {
	t.Helper()

	img, err := random.Image(64, 1)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	ref, err := goname.NewTag(host+"/"+repo+":latest", goname.Insecure)
	if err != nil {
		t.Fatalf("NewTag: %v", err)
	}
	if err := remote.Write(ref, img, ropts...); err != nil {
		t.Fatalf("writing image: %v", err)
	}
	if !withSigTag {
		return
	}

	hash, err := img.Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	sigImg, err := random.Image(64, 1)
	if err != nil {
		t.Fatalf("random.Image (sig): %v", err)
	}
	sigRef, err := goname.NewTag(host+"/"+repo+":"+strings.ReplaceAll(hash.String(), ":", "-")+".sig", goname.Insecure)
	if err != nil {
		t.Fatalf("NewTag (sig): %v", err)
	}
	if err := remote.Write(sigRef, sigImg, ropts...); err != nil {
		t.Fatalf("writing signature manifest: %v", err)
	}
}

// realVerifyError returns the error cosign's own VerifyImageSignatures produces
// for ref. The sentinels this test discriminates on hold an unexported err
// field, so a zero-value &ErrNoMatchingSignatures{} panics the moment anything
// formats it -- including fmt.Errorf("%w", ...), which calls Error() eagerly.
// Driving the real code path is also what keeps the test honest: it fails if
// cosign ever stops returning these types for these conditions, which a
// hand-built value would silently hide.
func realVerifyError(t *testing.T, ref string, ropts []remote.Option) error {
	t.Helper()

	rso, ro := testOpts()
	v, err := NewVerifier(context.Background(), Config{Key: writeTestPubKey(t)}, rso, ro)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	t.Cleanup(v.Close)

	// WithMoreRemoteOptions, not WithRemoteOptions: the latter overwrites ROpt
	// wholesale and would discard the remote.WithContext binding ClientOpts
	// installed, leaving cosign's registry reads outside the run's context.
	v.co.RegistryClientOpts = append(v.co.RegistryClientOpts, ociremote.WithMoreRemoteOptions(ropts...))

	r, err := reference.ParseReference(ref, goname.Insecure)
	if err != nil {
		t.Fatalf("ParseReference(%q): %v", ref, err)
	}
	_, _, err = cosignpkg.VerifyImageSignatures(context.Background(), r, v.co)
	if err == nil {
		t.Fatalf("VerifyImageSignatures(%q) succeeded against an unsigned fixture", ref)
	}
	return err
}

// The whole fallback design turns on this discrimination. "No classic
// signature exists" is fail-safe and may retry down the bundle path; "the
// signatures that exist did not validate" must not, or a bad signature gets a
// second chance to pass.
func TestFallbackEligibleRejectsInvalidSignatures(t *testing.T) {
	host, ropts := newSigTestRegistry(t)
	seedSigTestImage(t, host, "unsigned", false, ropts)
	seedSigTestImage(t, host, "badsig", true, ropts)

	noSigs := realVerifyError(t, host+"/unsigned:latest", ropts)
	tagNotFound := realVerifyError(t, host+"/absent:latest", ropts)
	noMatching := realVerifyError(t, host+"/badsig:latest", ropts)

	// Guard the fixtures: if a seeding change turned one of these into some
	// other error, the table below would still pass while testing nothing.
	for _, f := range []struct {
		name string
		err  error
		want any
	}{
		{"unsigned image", noSigs, new(*cosignpkg.ErrNoSignaturesFound)},
		{"absent tag", tagNotFound, new(*cosignpkg.ErrImageTagNotFound)},
		{"unverifiable signature", noMatching, new(*cosignpkg.ErrNoMatchingSignatures)},
	} {
		if !errors.As(f.err, f.want) {
			t.Fatalf("%s fixture produced %T (%v), want %T", f.name, f.err, f.err, f.want)
		}
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "no signatures found falls back", err: noSigs, want: true},
		{name: "missing signature tag falls back", err: tagNotFound, want: true},
		{name: "no MATCHING signatures must not fall back", err: noMatching, want: false},
		{name: "wrapped no-matching must not fall back", err: fmt.Errorf("verifying: %w", noMatching), want: false},
		{name: "wrapped no-signatures still falls back", err: fmt.Errorf("verifying: %w", noSigs), want: true},
		{name: "arbitrary transport error must not fall back", err: errors.New("connection refused"), want: false},
		{name: "nil must not fall back", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fallbackEligible(tt.err); got != tt.want {
				t.Fatalf("fallbackEligible(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Verify must surface a failed-validation error as-is. If it instead fell
// through to the bundle path, the returned error would be retry.Operation's
// "operation unsuccessful after N attempts" wrapper, which is not an
// *ErrNoMatchingSignatures.
func TestVerifyDoesNotFallBackOnInvalidSignature(t *testing.T) {
	host, ropts := newSigTestRegistry(t)
	seedSigTestImage(t, host, "badsig", true, ropts)

	rso, ro := testOpts()
	v, err := NewVerifier(context.Background(), Config{Key: writeTestPubKey(t)}, rso, ro)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	defer v.Close()
	v.co.RegistryClientOpts = append(v.co.RegistryClientOpts, ociremote.WithMoreRemoteOptions(ropts...))

	err = v.Verify(context.Background(), host+"/badsig:latest")
	if err == nil {
		t.Fatal("Verify accepted an image whose only signature failed validation")
	}
	var noMatching *cosignpkg.ErrNoMatchingSignatures
	if !errors.As(err, &noMatching) {
		t.Fatalf("Verify replaced the validation failure with %T (%v); it fell back", err, err)
	}
}

// discardCtx carries a no-op logger, since retry.Operation logs every failed
// attempt through log.FromContext.
func discardCtx() context.Context {
	return zerolog.New(io.Discard).WithContext(context.Background())
}

// countingTransport counts the requests whose path contains match, optionally
// failing them with status and running on afterwards.
type countingTransport struct {
	base   http.RoundTripper
	match  string
	status int
	on     func()
	n      atomic.Int32
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.Contains(req.URL.Path, t.match) {
		return t.base.RoundTrip(req)
	}
	t.n.Add(1)
	if t.on != nil {
		t.on()
	}
	if t.status == 0 {
		return t.base.RoundTrip(req)
	}
	return &http.Response{
		StatusCode: t.status,
		Status:     http.StatusText(t.status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// newCountingVerifier builds a keyed Verifier whose registry traffic runs
// through ct, on both the classic and the bundle path. Both need the append:
// NewVerifier takes coBundle's shallow copy before returning, so the slice
// header is already snapshotted and an append to one does not reach the other.
// The two may still write the same backing slot, which is harmless -- the value
// appended is identical.
//
// WithRemoteOptions, which replaces ROpt wholesale, is the only thing that
// works here: options.RegistryOptions.ClientOpts ends its list with
// remote.Reuse(puller) (cli/options/registry.go:169), and a reused Puller
// captured its transport when it was built, so a remote.WithTransport appended
// afterwards via WithMoreRemoteOptions is silently ignored. Dropping the
// inherited remote.WithContext along with the Reuse is deliberate: requests
// then run on context.Background, which is what lets
// TestVerifyRetriesRegistryFailures cancel the run context mid-request without
// aborting the request itself.
func newCountingVerifier(t *testing.T, retries int, ct *countingTransport) *Verifier {
	t.Helper()

	v, err := NewVerifier(context.Background(), Config{Key: writeTestPubKey(t)},
		&flags.StoreRootOpts{Retries: retries}, &flags.CliRootOpts{})
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	t.Cleanup(v.Close)
	route := ociremote.WithRemoteOptions(
		remote.WithTransport(ct),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	)
	v.co.RegistryClientOpts = append(v.co.RegistryClientOpts, route)
	v.coBundle.RegistryClientOpts = append(v.coBundle.RegistryClientOpts, route)
	return v
}

// A signature that was found and failed to validate must cost exactly one
// attempt however large --retries is, or the retry loop hands a bad signature
// N chances to pass -- the hazard fallbackEligible exists to prevent, arriving
// by a second route.
//
// Comparing two retry budgets against the same fixture, rather than asserting
// an absolute count, keeps this independent of how many round trips cosign
// makes per attempt.
func TestVerifyDoesNotRetryInvalidSignature(t *testing.T) {
	host, ropts := newSigTestRegistry(t)
	seedSigTestImage(t, host, "badsig", true, ropts)

	sigFetches := func(retries int) int32 {
		ct := &countingTransport{base: http.DefaultTransport, match: ".sig"}
		err := newCountingVerifier(t, retries, ct).Verify(discardCtx(), host+"/badsig:latest")
		var noMatching *cosignpkg.ErrNoMatchingSignatures
		if !errors.As(err, &noMatching) {
			t.Fatalf("Verify(retries=%d) returned %T (%v), want *ErrNoMatchingSignatures", retries, err, err)
		}
		return ct.n.Load()
	}

	one, many := sigFetches(1), sigFetches(5)
	if one == 0 {
		t.Fatal("fixture never fetched the signature manifest; the assertion below would be vacuous")
	}
	if many != one {
		t.Fatalf("--retries=5 cost %d signature-manifest fetches against --retries=1's %d: a failed validation is being retried", many, one)
	}
}

// The bundle CheckOpts must be the classic one with a single flag flipped.
// Rebuilding it instead would re-read the key and, keyless, refetch TUF; and
// the two must stay distinct objects, since a NewBundleFormat that leaked back
// into v.co would make VerifyImageSignatures reject every classic image
// outright (pkg/cosign/verify.go:645).
//
// reflect.DeepEqual is exact here despite CheckOpts holding func-valued
// options: its Slice and Ptr cases short-circuit on identical backing pointers,
// which a shallow copy guarantees, and the one func field cosign would trip
// over -- ClaimVerifier -- is nil on both, since only cosign's CLI commands
// ever set it.
func TestBundleCheckOptsIsClassicPlusFlag(t *testing.T) {
	rso, ro := testOpts()
	v, err := NewVerifier(context.Background(), Config{Key: writeTestPubKey(t)}, rso, ro)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	defer v.Close()

	if v.co.NewBundleFormat {
		t.Fatal("classic CheckOpts has NewBundleFormat set; VerifyImageSignatures rejects it outright")
	}
	if !v.coBundle.NewBundleFormat {
		t.Fatal("bundle CheckOpts has NewBundleFormat clear; VerifyImageAttestations would take the tag-based branch")
	}
	if v.co == v.coBundle {
		t.Fatal("both paths share one CheckOpts; the flag cannot differ")
	}

	probe := *v.co
	probe.NewBundleFormat = true
	if !reflect.DeepEqual(&probe, v.coBundle) {
		t.Fatal("bundle CheckOpts differs from the classic one in more than NewBundleFormat; " +
			"it is no longer a shallow copy and the two paths may disagree on trust material")
	}
}

// The fallback must land in cosign's library bundle path. ErrNoMatchingAttestations
// is the proof: nothing VerifyImageSignatures reaches constructs that type, so
// the classic path cannot have returned it. The referrers count
// then pins that the request went out on the CheckOpts NewVerifier built: a
// fallback that rebuilt its own registry options, or shelled back out to the
// CLI, would reach the registry by a transport this test never sees.
func TestVerifyFallsBackToLibraryBundlePath(t *testing.T) {
	host, ropts := newSigTestRegistry(t)
	seedSigTestImage(t, host, "unsigned", false, ropts)

	ct := &countingTransport{base: http.DefaultTransport, match: "/referrers/"}
	err := newCountingVerifier(t, 1, ct).Verify(discardCtx(), host+"/unsigned:latest")

	var noAtts *cosignpkg.ErrNoMatchingAttestations
	if !errors.As(err, &noAtts) {
		t.Fatalf("Verify returned %T (%v), want *ErrNoMatchingAttestations from the bundle path", err, err)
	}
	if ct.n.Load() == 0 {
		t.Fatal("no referrers request reached this Verifier's transport; the bundle path used registry options of its own")
	}
}

// The bundle path needs its own --retries budget. Nothing else supplies one now
// that it is a bare library call, and without it a transient registry failure
// during bundle verification silently drops a signed image.
//
// The cancel-from-inside-the-request trick is explained on
// TestVerifyRetriesRegistryFailures. Failing only the referrers request leaves
// the classic attempt intact, so the run reaches verifyBundle by the ordinary
// route: no classic signature exists, which is fallback-eligible.
//
// 403 is the status that surfaces. go-containerregistry accepts 400, 404 and
// 406 on this endpoint as "no Referrers API here" and silently retries the
// fallback tag scheme (remote/referrers.go:64), and it retries 5xx itself,
// which would make this pass without hauler's loop existing at all.
func TestVerifyBundleRetriesRegistryFailures(t *testing.T) {
	host, ropts := newSigTestRegistry(t)
	seedSigTestImage(t, host, "unsigned", false, ropts)

	ctx, cancel := context.WithCancel(discardCtx())
	defer cancel()

	ct := &countingTransport{base: http.DefaultTransport, match: "/referrers/", status: http.StatusForbidden, on: cancel}
	err := newCountingVerifier(t, 3, ct).Verify(ctx, host+"/unsigned:latest")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify returned %T (%v), want context.Canceled from retry.Operation's backoff; the bundle path is not being retried", err, err)
	}
	if ct.n.Load() == 0 {
		t.Fatal("no referrers request reached the transport")
	}
}

// A bundle verification that ends in ErrNoMatchingAttestations must cost one
// attempt however large --retries is. Cosign spells both "no bundle exists" and
// "every bundle found failed to verify" with that one error, and the second is
// the hazard: retrying it hands a bad bundle N chances to pass, the same hazard
// fallbackEligible guards on the classic side.
func TestVerifyBundleDoesNotRetryMissingBundles(t *testing.T) {
	host, ropts := newSigTestRegistry(t)
	seedSigTestImage(t, host, "unsigned", false, ropts)

	referrerFetches := func(retries int) int32 {
		ct := &countingTransport{base: http.DefaultTransport, match: "/referrers/"}
		err := newCountingVerifier(t, retries, ct).Verify(discardCtx(), host+"/unsigned:latest")
		var noAtts *cosignpkg.ErrNoMatchingAttestations
		if !errors.As(err, &noAtts) {
			t.Fatalf("Verify(retries=%d) returned %T (%v), want *ErrNoMatchingAttestations", retries, err, err)
		}
		return ct.n.Load()
	}

	one, many := referrerFetches(1), referrerFetches(3)
	if one == 0 {
		t.Fatal("fixture never issued a referrers request; the assertion below would be vacuous")
	}
	if many != one {
		t.Fatalf("--retries=3 cost %d referrers requests against --retries=1's %d: a terminal bundle result is being retried", many, one)
	}
}

// --retries must keep covering verification. The library path replaced a CLI
// call that retried internally, so dropping the loop would silently turn
// --retries into a no-op for signatures.
//
// Cancelling the run context from inside the first failing request is what
// makes a second attempt observable without waiting out consts.RetriesInterval:
// VerifyImageSignatures never sees this context -- its registry options carry
// the one NewVerifier was built with -- so a context.Canceled can only have
// come from retry.Operation's backoff, which is reached only when the error was
// judged retryable and a further attempt was pending.
func TestVerifyRetriesRegistryFailures(t *testing.T) {
	host, _ := newSigTestRegistry(t)

	ctx, cancel := context.WithCancel(discardCtx())
	defer cancel()

	// 400, not 500: go-containerregistry retries 5xx itself, which would make
	// this pass without hauler's loop existing at all.
	ct := &countingTransport{base: http.DefaultTransport, match: "/manifests/", status: http.StatusBadRequest, on: cancel}
	err := newCountingVerifier(t, 5, ct).Verify(ctx, host+"/anything:latest")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify returned %T (%v), want context.Canceled from retry.Operation's backoff; a registry failure is not being retried", err, err)
	}
	if ct.n.Load() == 0 {
		t.Fatal("no manifest request reached the transport")
	}
}
