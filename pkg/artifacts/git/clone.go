package git

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	gogit "github.com/go-git/go-git/v5"
	gitclient "github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"hauler.dev/go/hauler/v2/pkg/content"
)

// cloneAuth holds the credentials compute uses to clone a remote URL, set via the WithUsername/WithPassword/WithCertFile/WithKeyFile/WithCaFile/WithInsecureSkipTLSVerify/WithSSHKey options.
type cloneAuth struct {
	username string
	password string
	certFile string
	keyFile  string
	caFile   string
	sshKey   string

	insecureSkipTLSVerify bool
}

// IsGitURL reports whether path is a remote git URL (https://, ssh://, or the git@host:path shorthand) rather than a local filesystem path.
func IsGitURL(path string) bool {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "ssh://") {
		return true
	}
	if at := strings.Index(path, "@"); at > 0 {
		return strings.Contains(path[at:], ":")
	}
	return false
}

// IsNonBareRepo reports whether path is a local, non-bare git repository, one with its internals nested under a .git directory rather than sitting at the top level.
func IsNonBareRepo(path string) bool {
	if IsGitURL(path) {
		return false
	}
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// deriveGitName turns a clone URL into a reasonable artifact name: the last path segment, minus a trailing ".git", covering https://, ssh://, and the git@host:path shorthand.
func deriveGitName(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	} else if i := strings.Index(s, "@"); i >= 0 {
		s = strings.Replace(s[i+1:], ":", "/", 1)
	}
	s = strings.TrimSuffix(s, "/")

	base := s
	if i := strings.LastIndex(s, "/"); i >= 0 {
		base = s[i+1:]
	}
	return strings.TrimSuffix(base, ".git")
}

// clone clones url into a fresh temp directory as a bare repo using g.auth, and returns that directory plus a cleanup func.
func (g *Git) clone(url string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "hauler-git-clone")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }

	auth := g.auth
	if auth.certFile != "" || auth.keyFile != "" || auth.caFile != "" || auth.insecureSkipTLSVerify {
		if err := installGitHTTPClient(auth); err != nil {
			cleanup()
			return "", nil, err
		}
	}

	opts := &gogit.CloneOptions{URL: url}
	switch {
	case auth.username != "" || auth.password != "":
		opts.Auth = &githttp.BasicAuth{Username: auth.username, Password: auth.password}
	case auth.sshKey != "":
		sshAuth, err := gitssh.NewPublicKeysFromFile("git", auth.sshKey, "")
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("loading SSH key [%s]: %w", auth.sshKey, err)
		}
		opts.Auth = sshAuth
	}
	// Neither case leaves Auth nil: a public HTTPS repo, or an SSH URL falling back to ssh-agent/default key discovery, same as the git CLI.

	if _, err := gogit.PlainCloneContext(g.ctx, dir, true, opts); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("cloning [%s]: %w", url, err)
	}

	return dir, cleanup, nil
}

// installGitHTTPClient points go-git's https transport at a client built from auth's TLS fields, since go-git only supports registering a client per scheme, process-wide, not per clone call.
func installGitHTTPClient(auth cloneAuth) error {
	if auth.certFile == "" || auth.keyFile == "" {
		tr, err := content.BuildTransport(auth.insecureSkipTLSVerify, auth.caFile)
		if err != nil {
			return err
		}
		gitclient.InstallProtocol("https", githttp.NewClient(&http.Client{Transport: tr}))
		return nil
	}

	// Always clone our own transport here, since content.BuildTransport can hand back the shared remote.DefaultTransport unmodified when neither caFile nor insecureSkipTLSVerify is set, and we're about to add a client cert to it.
	base, ok := remote.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("unexpected default transport type %T", remote.DefaultTransport)
	}
	tr := base.Clone()
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	} else {
		tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	}

	if auth.insecureSkipTLSVerify {
		tr.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec
	} else if auth.caFile != "" {
		caTr, err := content.BuildTransport(false, auth.caFile)
		if err != nil {
			return err
		}
		if caHTTPTr, ok := caTr.(*http.Transport); ok && caHTTPTr.TLSClientConfig != nil {
			tr.TLSClientConfig.RootCAs = caHTTPTr.TLSClientConfig.RootCAs
		}
	}

	cert, err := tls.LoadX509KeyPair(auth.certFile, auth.keyFile)
	if err != nil {
		return fmt.Errorf("loading client certificate: %w", err)
	}
	tr.TLSClientConfig.Certificates = append(tr.TLSClientConfig.Certificates, cert)

	gitclient.InstallProtocol("https", githttp.NewClient(&http.Client{Transport: tr}))
	return nil
}
