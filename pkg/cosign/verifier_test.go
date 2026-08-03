package cosign

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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

	c := NewCache()
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

	// Tlog: true sends SetLegacyClientsAndKeys after the Rekor public keys over
	// TUF, so this build fails without network. Asserting on the entry count
	// rather than on the returned pointer keeps the check meaningful offline: a
	// failed build is still cached under its own Config, so the count rises iff
	// the cache key covers Tlog, whether or not sigstore was reachable.
	_, _ = c.Get(context.Background(), Config{Key: keyPath, Tlog: true})
	if len(c.m) != 3 {
		t.Fatalf("a Config differing only in Tlog produced %d cache entries, want 3; the key is too coarse", len(c.m))
	}
}

func TestCacheGetIsConcurrencySafe(t *testing.T) {
	keyPath := writeTestPubKey(t)
	c := NewCache()
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
	c := NewCache()
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
			_, err := NewVerifier(context.Background(), tt.cfg)
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
			v, err := NewVerifier(context.Background(), tt.cfg)
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
	if _, err := NewVerifier(context.Background(), Config{Key: writeTestPubKey(t)}); err != nil {
		t.Fatalf("NewVerifier rejected a plain keyed Config: %v", err)
	}
}
