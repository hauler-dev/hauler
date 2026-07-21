package content

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	"github.com/containerd/containerd/v2/core/remotes"
	cdocker "github.com/containerd/containerd/v2/core/remotes/docker"
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

// plainHTTPRoundTripper rewrites outgoing https:// requests to http:// for a
// single registry authority (host:port). This is required when PlainHTTP is
// set: the Docker authorizer follows the Bearer realm URL from the
// WWW-Authenticate header literally, and registries like Harbor always
// advertise an https:// realm regardless of the incoming transport, so the
// token fetch fails with "server gave HTTP response to HTTPS client" unless
// the scheme is rewritten before the request leaves the client.
//
// The rewrite is scoped to the registry's exact authority on purpose: a
// plain-http registry may legitimately 301-redirect blob fetches to a real
// HTTPS object store or CDN on a different host (or even a different port on
// the same host), and those must NOT be downgraded. host must already be a
// bare authority (no path) -- NewRegistryHTTPClient strips any path before
// building this struct, since req.URL.Host is never anything but the authority.
type plainHTTPRoundTripper struct {
	inner http.RoundTripper
	host  string // registry authority (host:port), the only host we downgrade
}

func (r plainHTTPRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "https" && req.URL.Host == r.host {
		reqCopy := req.Clone(req.Context())
		reqCopy.URL.Scheme = "http"
		req = reqCopy
	}
	return r.inner.RoundTrip(req)
}

// NewRegistryHTTPClient builds an *http.Client configured for opts, cloning
// http.DefaultTransport rather than mutating it in place, which would leak
// InsecureSkipVerify into every other HTTP client in the process.
//
// host is the registry this client talks to (e.g. "localhost:5000"). Callers
// such as cmd/hauler/cli/store/copy.go derive it from a target reference's
// remainder after "://", so it may arrive as "host:port/repo/path"; any path
// is stripped to the bare authority before use. When opts.PlainHTTP is set,
// that authority scopes a targeted https->http rewrite (see
// plainHTTPRoundTripper) so a legitimate cross-host https redirect (e.g. to a
// CDN or object store) is not downgraded.
//
// Build this once and share it across all RegistryTargets for a copy: a
// transport per target defeats connection pooling and can exhaust file
// descriptors on large copies.
func NewRegistryHTTPClient(host string, opts RegistryOptions) *http.Client {
	var transport *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = dt.Clone()
	} else {
		// Replaced by instrumentation or a test harness.
		transport = &http.Transport{}
	}
	if opts.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	var rt http.RoundTripper = transport
	if opts.PlainHTTP {
		// host may arrive as "registry:port/repo/path" (copy.go passes
		// components[1]); req.URL.Host is only ever the authority, so match on that.
		authority, _, _ := strings.Cut(host, "/")
		rt = plainHTTPRoundTripper{inner: transport, host: authority}
	}
	return &http.Client{Transport: rt}
}

// NewRegistryTarget returns a RegistryTarget that pushes to host (e.g. "localhost:5000").
// client must also back the authorizer: otherwise Bearer token fetches fall back to
// http.DefaultClient and ignore opts.Insecure.
func NewRegistryTarget(host string, opts RegistryOptions, client *http.Client) *RegistryTarget {
	authorizer := cdocker.NewDockerAuthorizer(
		cdocker.WithAuthClient(client),
		cdocker.WithAuthCreds(func(h string) (string, string, error) {
			if opts.Username != "" {
				return opts.Username, opts.Password, nil
			}
			// Bridge to go-containerregistry's keychain for credential lookup.
			reg, err := goname.NewRegistry(h, goname.Insecure)
			if err != nil {
				return "", "", fmt.Errorf("parsing registry host [%s] for credential lookup: %w", h, err)
			}
			a, err := goauthn.DefaultKeychain.Resolve(reg)
			if err != nil {
				// don't fall back to anonymous on a real resolution error
				return "", "", fmt.Errorf("resolving credentials for [%s]: %w", h, err)
			}
			if a == goauthn.Anonymous {
				return "", "", nil
			}
			cfg, err := a.Authorization()
			if err != nil {
				return "", "", fmt.Errorf("reading resolved authorization for [%s]: %w", h, err)
			}
			return cfg.Username, cfg.Password, nil
		}),
	)

	hosts := func(h string) ([]cdocker.RegistryHost, error) {
		host, err := cdocker.DefaultHost(h)
		if err != nil {
			return nil, err
		}
		scheme := "https"
		if opts.PlainHTTP {
			scheme = "http"
		}
		return []cdocker.RegistryHost{{
			Client:       client,
			Authorizer:   authorizer,
			Scheme:       scheme,
			Host:         host,
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

// Resolve and Fetcher exist only to satisfy the Target interface; Hauler never
// reads from a registry through RegistryTarget (store copy resolves and fetches
// from the local OCI layout and uses this target only for Pusher, and image
// pulls go through go-containerregistry). Note that the underlying containerd v2
// docker resolver no longer converts legacy Docker Schema1 manifests on the read
// path (it returns ErrNotImplemented), so wiring these into a pull-from-registry
// flow would not handle Schema1 sources.
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
