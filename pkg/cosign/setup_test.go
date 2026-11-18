package cosign

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/cosign/v3/pkg/cosign/env"
)

// newTestCert signs a CA certificate with parent, or self-signs it when parent is nil.
func newTestCert(t *testing.T, cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}
	return cert, key
}

// writeTestRootFile writes a root plus an intermediate to one pem file, the shape SIGSTORE_ROOT_FILE takes, and returns the expected pools.
func writeTestRootFile(t *testing.T) (string, *x509.CertPool, *x509.CertPool) {
	t.Helper()
	root, rootKey := newTestCert(t, "test root", nil, nil)
	inter, _ := newTestCert(t, "test intermediate", root, rootKey)

	var buf []byte
	for _, c := range []*x509.Certificate{root, inter} {
		buf = append(buf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	path := filepath.Join(t.TempDir(), "fulcio.pem")
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("writing root file: %v", err)
	}

	roots, inters := x509.NewCertPool(), x509.NewCertPool()
	roots.AddCert(root)
	inters.AddCert(inter)
	return path, roots, inters
}

// Air-gapped users point SIGSTORE_* at their own sigstore, so a keyless verifier must build from those files with no tuf fetch.
func TestKeylessUsesSigstoreOverrides(t *testing.T) {
	rootFile, wantRoots, wantInters := writeTestRootFile(t)
	pubKey := writeTestPubKey(t)
	t.Setenv(env.VariableSigstoreRootFile.String(), rootFile)
	t.Setenv(env.VariableSigstoreRekorPublicKey.String(), pubKey)
	t.Setenv(env.VariableSigstoreCTLogPublicKeyFile.String(), pubKey)

	rso, ro := testOpts()
	v, err := NewVerifier(context.Background(), Config{CertIdentity: "me@example.com", CertOidcIssuer: "https://issuer.example.com"}, rso, ro)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	defer v.Close()

	if v.co.TrustedMaterial != nil {
		t.Fatal("trusted material was loaded from tuf despite SIGSTORE_* overrides")
	}
	if !v.co.RootCerts.Equal(wantRoots) {
		t.Fatal("fulcio roots were not loaded from SIGSTORE_ROOT_FILE")
	}
	if !v.co.IntermediateCerts.Equal(wantInters) {
		t.Fatal("fulcio intermediates were not split out of SIGSTORE_ROOT_FILE")
	}
	if v.co.RekorPubKeys == nil || len(v.co.RekorPubKeys.Keys) == 0 {
		t.Fatal("rekor public key was not loaded from SIGSTORE_REKOR_PUBLIC_KEY")
	}
	if v.co.CTLogPubKeys == nil || len(v.co.CTLogPubKeys.Keys) == 0 {
		t.Fatal("ctlog public key was not loaded from SIGSTORE_CT_LOG_PUBLIC_KEY_FILE")
	}
}

// A missing SIGSTORE_ROOT_FILE must fail rather than fall back to the public fulcio roots.
func TestFulcioRootsRejectsMissingRootFile(t *testing.T) {
	t.Setenv(env.VariableSigstoreRootFile.String(), filepath.Join(t.TempDir(), "missing.pem"))
	if _, _, err := fulcioRoots(); err == nil {
		t.Fatal("fulcioRoots accepted a missing SIGSTORE_ROOT_FILE")
	}
}

// Every key reference cosign accepts must still reach cosign instead of being read as a file path.
func TestKeyReferenceSchemesReachCosign(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		raw, err := os.ReadFile(writeTestPubKey(t))
		if err != nil {
			t.Fatalf("reading key: %v", err)
		}
		t.Setenv("HAULER_TEST_COSIGN_PUB", string(raw))
		if _, _, err := loadKeyVerifier(context.Background(), "env://HAULER_TEST_COSIGN_PUB"); err != nil {
			t.Fatalf("env:// key was rejected: %v", err)
		}
	})

	t.Run("gitlab", func(t *testing.T) {
		t.Setenv(env.VariableGitLabToken.String(), "")
		os.Unsetenv(env.VariableGitLabToken.String())
		_, _, err := loadKeyVerifier(context.Background(), "gitlab://example/repo")
		if err == nil || !strings.Contains(err.Error(), env.VariableGitLabToken.String()) {
			t.Fatalf("gitlab:// key did not reach cosign's gitlab provider: %v", err)
		}
	})
}

// insecureSkipTLSVerify takes precedence over caFile, and a bad caFile fails instead of being silently ignored.
func TestRegistryClientOptsCaFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-ca.pem")
	if _, err := registryClientOpts(context.Background(), Config{CaFile: missing}); err == nil {
		t.Fatal("registryClientOpts accepted a missing ca file")
	}
	if _, err := registryClientOpts(context.Background(), Config{CaFile: missing, InsecureSkipTLSVerify: true}); err != nil {
		t.Fatalf("insecureSkipTLSVerify did not take precedence over ca file: %v", err)
	}
}
