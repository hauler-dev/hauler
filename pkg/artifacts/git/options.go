package git

import (
	"context"

	"hauler.dev/go/hauler/v2/pkg/getter"
)

type Option func(*Git)

func WithClient(c *getter.Client) Option {
	return func(g *Git) {
		g.client = c
	}
}

func WithContext(ctx context.Context) Option {
	return func(g *Git) {
		g.ctx = ctx
	}
}

// WithName overrides the derived artifact name, taking precedence over both a URL-derived name and the client's own naming.
func WithName(name string) Option {
	return func(g *Git) {
		g.nameOverride = name
	}
}

// WithUsername sets the username used to clone a https:// URL.
func WithUsername(username string) Option {
	return func(g *Git) {
		g.auth.username = username
	}
}

// WithPassword sets the password or access token used to clone a https:// URL.
func WithPassword(password string) Option {
	return func(g *Git) {
		g.auth.password = password
	}
}

// WithCertFile sets the TLS client certificate used to clone a https:// URL.
func WithCertFile(certFile string) Option {
	return func(g *Git) {
		g.auth.certFile = certFile
	}
}

// WithKeyFile sets the TLS client key used to clone a https:// URL.
func WithKeyFile(keyFile string) Option {
	return func(g *Git) {
		g.auth.keyFile = keyFile
	}
}

// WithCaFile sets the CA bundle used to verify a https:// URL's certificate.
func WithCaFile(caFile string) Option {
	return func(g *Git) {
		g.auth.caFile = caFile
	}
}

// WithInsecureSkipTLSVerify disables TLS certificate verification when cloning a https:// URL.
func WithInsecureSkipTLSVerify(insecure bool) Option {
	return func(g *Git) {
		g.auth.insecureSkipTLSVerify = insecure
	}
}

// WithSSHKey sets the private key used to clone a git@/ssh:// URL, falling back to ssh-agent/default key discovery when unset.
func WithSSHKey(sshKey string) Option {
	return func(g *Git) {
		g.auth.sshKey = sshKey
	}
}
