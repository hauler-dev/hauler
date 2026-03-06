package content

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/containerd/containerd/remotes"
	cdocker "github.com/containerd/containerd/remotes/docker"
	goauthn "github.com/google/go-containerregistry/pkg/authn"
	goname "github.com/google/go-containerregistry/pkg/name"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

var _ Target = (*RegistryTarget)(nil)

// RegistryTarget implements Target for pushing to a remote OCI registry.
// Authentication is sourced from the local Docker credential store via go-containerregistry's
// default keychain unless explicit credentials are provided in RegistryOptions.
type RegistryTarget struct {
	resolver remotes.Resolver
}

// NewRegistryTarget returns a RegistryTarget that pushes to host (e.g. "localhost:5000").
func NewRegistryTarget(host string, opts RegistryOptions) *RegistryTarget {
	authorizer := cdocker.NewDockerAuthorizer(
		cdocker.WithAuthCreds(func(h string) (string, string, error) {
			if opts.Username != "" {
				return opts.Username, opts.Password, nil
			}
			// Bridge to go-containerregistry's keychain for credential lookup.
			reg, err := goname.NewRegistry(h, goname.Insecure)
			if err != nil {
				return "", "", nil
			}
			a, err := goauthn.DefaultKeychain.Resolve(reg)
			if err != nil || a == goauthn.Anonymous {
				return "", "", nil
			}
			cfg, err := a.Authorization()
			if err != nil {
				return "", "", nil
			}
			return cfg.Username, cfg.Password, nil
		}),
	)

	hosts := func(h string) ([]cdocker.RegistryHost, error) {
		scheme := "https"
		if opts.PlainHTTP || opts.Insecure {
			scheme = "http"
		}
		return []cdocker.RegistryHost{{
			Client:       http.DefaultClient,
			Authorizer:   authorizer,
			Scheme:       scheme,
			Host:         h,
			Path:         "/v2",
			Capabilities: cdocker.HostCapabilityPull | cdocker.HostCapabilityResolve | cdocker.HostCapabilityPush,
		}}, nil
	}

	return &RegistryTarget{
		resolver: cdocker.NewResolver(cdocker.ResolverOptions{
			Hosts: hosts,
		}),
	}
}

func (t *RegistryTarget) Resolve(ctx context.Context, ref string) (ocispec.Descriptor, error) {
	_, desc, err := t.resolver.Resolve(ctx, ref)
	return desc, err
}

func (t *RegistryTarget) Fetcher(ctx context.Context, ref string) (remotes.Fetcher, error) {
	return t.resolver.Fetcher(ctx, ref)
}

func (t *RegistryTarget) Pusher(ctx context.Context, ref string) (remotes.Pusher, error) {
	return t.resolver.Pusher(ctx, ref)
}

// RewriteRefToRegistry rewrites sourceRef to use targetRegistry as its host, preserving the
// repository path and tag or digest. For example:
//
//	"index.docker.io/library/nginx:latest" + "localhost:5000" → "localhost:5000/library/nginx:latest"
func RewriteRefToRegistry(sourceRef string, targetRegistry string) (string, error) {
	ref, err := goname.ParseReference(sourceRef)
	if err != nil {
		return "", fmt.Errorf("parsing reference %q: %w", sourceRef, err)
	}
	repo := strings.TrimPrefix(ref.Context().RepositoryStr(), "/")
	switch r := ref.(type) {
	case goname.Tag:
		return fmt.Sprintf("%s/%s:%s", targetRegistry, repo, r.TagStr()), nil
	case goname.Digest:
		return fmt.Sprintf("%s/%s@%s", targetRegistry, repo, r.DigestStr()), nil
	default:
		return fmt.Sprintf("%s/%s:latest", targetRegistry, repo), nil
	}
}
