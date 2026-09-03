package chart

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
)

type staticKeychain struct{}

func (staticKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.FromConfig(authn.AuthConfig{Username: "user", Password: "password"}), nil
}

func TestResolveRepoCredentials(t *testing.T) {
	original := chartKeychain
	chartKeychain = staticKeychain{}
	t.Cleanup(func() { chartKeychain = original })

	username, password, err := resolveRepoCredentials("https://my.registry.com/charts")
	if err != nil {
		t.Fatalf("resolveRepoCredentials: %v", err)
	}
	if username != "user" || password != "password" {
		t.Fatalf("credentials = (%q, %q), want (user, password)", username, password)
	}
}

func TestResolveRepoCredentialsIgnoresNonURL(t *testing.T) {
	username, password, err := resolveRepoCredentials("charts")
	if err != nil {
		t.Fatalf("resolveRepoCredentials: %v", err)
	}
	if username != "" || password != "" {
		t.Fatalf("credentials = (%q, %q), want empty credentials", username, password)
	}
}
