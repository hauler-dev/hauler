package content

import (
	"os"
	"os/user"
	"path/filepath"
)

// currentUser is a seam so tests can simulate a running UID with no passwd
// entry (where user.Current() returns an error).
var currentUser = user.Current

// SetDefaultDockerConfig defaults the DOCKER_CONFIG environment variable to the
// directory where `hauler login` (crane -> docker/cli) writes credentials, so
// that go-containerregistry's authn.DefaultKeychain -- used to resolve registry
// credentials during `hauler store copy registry://`, `add`, and `sync` -- can
// find them even when $HOME is unset.
//
// It replicates docker/cli's config.Dir()/getHomeDir() resolution using only the
// standard library (no docker/cli dependency): DOCKER_CONFIG if already set,
// otherwise <home>/.docker, where home is os.UserHomeDir() ($HOME on Unix,
// %USERPROFILE% on Windows) with an /etc/passwd fallback via os/user.Current()
// when $HOME is empty -- exactly the passwd fallback `hauler login` uses.
//
// When no home directory can be resolved at all ($HOME empty AND
// user.Current() fails or returns an empty HomeDir), this mirrors docker/cli's
// own config.Dir(), which computes filepath.Join(getHomeDir(), ".docker"): with
// getHomeDir() == "", that join collapses to the relative path ".docker". So
// DOCKER_CONFIG is defaulted to the relative path ".docker" in that case too,
// keeping `hauler login`'s (relative) write and the keychain's (relative) read
// in agreement as long as both run from the same working directory.
//
// The only remaining no-op case is when DOCKER_CONFIG is already explicitly
// set (an explicit value always wins). Setting DOCKER_CONFIG does not disable
// DefaultKeychain's $REGISTRY_AUTH_FILE / Podman auth.json fallbacks: those
// still apply when no config.json exists at the set path.
//
// Returns the directory it set and true, or "" and false if it made no change.
func SetDefaultDockerConfig() (string, bool) {
	if os.Getenv("DOCKER_CONFIG") != "" {
		return "", false
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if u, uerr := currentUser(); uerr == nil {
			home = u.HomeDir
		}
	}

	dir := filepath.Join(home, ".docker")
	os.Setenv("DOCKER_CONFIG", dir)
	return dir, true
}
