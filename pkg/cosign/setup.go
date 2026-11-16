package cosign

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	goname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	cosignpkg "github.com/sigstore/cosign/v3/pkg/cosign"
	"github.com/sigstore/cosign/v3/pkg/cosign/env"
	"github.com/sigstore/cosign/v3/pkg/cosign/pkcs11key"
	ociremote "github.com/sigstore/cosign/v3/pkg/oci/remote"
	csignature "github.com/sigstore/cosign/v3/pkg/signature"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	//lint:ignore SA1019 cosign's own fulcioroots fallback still uses this package and has no exported replacement
	"github.com/sigstore/sigstore/pkg/fulcioroots"
	"github.com/sigstore/sigstore/pkg/signature"

	"hauler.dev/go/hauler/v2/pkg/content"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// These helpers replace cosign's cmd/cosign/cli/options and cmd/cosign/cli/verify setup calls, which linked opa, cue, and the cloud registry credential helpers into hauler.

// keylessIdentities replaces options.CertVerifyOptions.Identities and keeps its error text.
func keylessIdentities(cfg Config) ([]cosignpkg.Identity, error) {
	if cfg.CertIdentity == "" && cfg.CertIdentityRegexp == "" {
		return nil, errors.New("--certificate-identity or --certificate-identity-regexp is required for verification in keyless mode")
	}
	if cfg.CertOidcIssuer == "" && cfg.CertOidcIssuerRegexp == "" {
		return nil, errors.New("--certificate-oidc-issuer or --certificate-oidc-issuer-regexp is required for verification in keyless mode")
	}
	return []cosignpkg.Identity{{IssuerRegExp: cfg.CertOidcIssuerRegexp, Issuer: cfg.CertOidcIssuer, SubjectRegExp: cfg.CertIdentityRegexp, Subject: cfg.CertIdentity}}, nil
}

// registryClientOpts replaces options.RegistryOptions.ClientOpts using the same transport hauler pulls images with.
func registryClientOpts(ctx context.Context, cfg Config) ([]ociremote.Option, error) {
	tr, err := content.BuildTransport(cfg.InsecureSkipTLSVerify, cfg.CaFile)
	if err != nil {
		return nil, err
	}
	ropts := []remote.Option{remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithTransport(tr)}
	// one reused puller shares its authenticated transport across every signature fetch
	if puller, err := remote.NewPuller(ropts...); err == nil {
		ropts = append(ropts, remote.Reuse(puller))
	}

	opts := []ociremote.Option{ociremote.WithRemoteOptions(ropts...)}
	target, err := ociremote.GetEnvTargetRepository()
	if err != nil {
		return nil, err
	}
	if (target != goname.Repository{}) {
		opts = append(opts, ociremote.WithTargetRepository(target))
	}
	return opts, nil
}

// setTrustedMaterial replaces verify.SetTrustedMaterial for hauler's inputs, skipping tuf when the SIGSTORE_* overrides supply the trust material instead.
func setTrustedMaterial(ctx context.Context, offlineWithKey bool, co *cosignpkg.CheckOpts) {
	if offlineWithKey {
		return
	}
	for _, v := range []env.Variable{env.VariableSigstoreCTLogPublicKeyFile, env.VariableSigstoreRootFile, env.VariableSigstoreRekorPublicKey, env.VariableSigstoreTSACertificateFile} {
		if env.Getenv(v) != "" {
			return
		}
	}
	tm, err := cosignpkg.TrustedRoot()
	if err != nil {
		log.FromContext(ctx).Warnf("unable to fetch [trusted_root.json] from tuf: %v... continuing with individual targets...", err)
		return
	}
	co.TrustedMaterial = tm
}

// setLegacyKeys replaces verify.SetLegacyClientsAndKeys for hauler's inputs, loading individual keys only when no trusted root was set.
func setLegacyKeys(ctx context.Context, keyless bool, co *cosignpkg.CheckOpts) error {
	if co.TrustedMaterial != nil {
		return nil
	}
	var err error
	if !co.IgnoreTlog {
		if co.RekorPubKeys, err = cosignpkg.GetRekorPubs(ctx); err != nil {
			return fmt.Errorf("failed to get [rekor] public keys: %w", err)
		}
	}
	if !keyless {
		return nil
	}
	if co.CTLogPubKeys, err = cosignpkg.GetCTLogPubs(ctx); err != nil {
		return fmt.Errorf("failed to get [ctlog] public keys: %w", err)
	}
	if co.RootCerts, co.IntermediateCerts, err = fulcioRoots(); err != nil {
		return fmt.Errorf("failed to get [fulcio] roots: %w", err)
	}
	return nil
}

// fulcioRoots replaces cosign's internal fulcioroots loader, preferring SIGSTORE_ROOT_FILE over the tuf roots.
func fulcioRoots() (*x509.CertPool, *x509.CertPool, error) {
	path := env.Getenv(env.VariableSigstoreRootFile)
	if path == "" {
		roots, err := fulcioroots.Get()
		if err != nil {
			return nil, nil, err
		}
		intermediates, err := fulcioroots.GetIntermediates()
		if err != nil {
			return nil, nil, err
		}
		return roots, intermediates, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read [%s]: %w", path, err)
	}
	certs, err := cryptoutils.UnmarshalCertificatesFromPEM(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse [%s]: %w", path, err)
	}
	roots := x509.NewCertPool()
	// intermediates stays nil when the file holds none, same as cosign
	var intermediates *x509.CertPool
	for _, cert := range certs {
		// root certificates are self-signed
		if bytes.Equal(cert.RawSubject, cert.RawIssuer) {
			roots.AddCert(cert)
			continue
		}
		if intermediates == nil {
			intermediates = x509.NewCertPool()
		}
		intermediates.AddCert(cert)
	}
	return roots, intermediates, nil
}

// loadKeyVerifier replaces verify.LoadVerifierFromKeyOrCert for a key, keeping every key reference cosign accepts (file, url, env, kms, k8s, gitlab, pkcs11).
func loadKeyVerifier(ctx context.Context, keyRef string) (signature.Verifier, func(), error) {
	sv, err := csignature.PublicKeyFromKeyRefWithHashAlgo(ctx, keyRef, crypto.SHA256)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load public key: %w", err)
	}
	if k, ok := sv.(*pkcs11key.Key); ok {
		return sv, k.Close, nil
	}
	return sv, func() {}, nil
}
