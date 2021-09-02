package store

import (
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"io"
	"net/http"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	godigest "github.com/opencontainers/go-digest"
)

func ParseRepoAndReference(repo string, reference string, opts ...name.Option) (name.Reference, error) {
	if d, err := godigest.Parse(reference); err == nil {
		return name.ParseReference(repo+"@"+d.String(), opts...)
	}
	return name.ParseReference(repo+":"+reference, opts...)
}

// TODO: This doesn't work
func FetchToken(ref name.Reference) error {
	fmt.Println(ref.Context().RepositoryStr())
	repo, err := name.NewRepository(ref.Context().RepositoryStr(), name.WithDefaultRegistry("ghcr.io"))
	if err != nil {
		return err
	}

	auth, err := authn.DefaultKeychain.Resolve(repo.Registry)
	if err != nil {
		return err
	}

	scopes := []string{repo.Scope(transport.PullScope)}
	t, err := transport.New(repo.Registry, auth, http.DefaultTransport, scopes)
	if err != nil {
		return err
	}

	client := &http.Client{Transport: t}

	resp, err := client.Get(fmt.Sprintf("%s://docker.io/v2/%s/tags/list", ref.Context().Scheme(), ref.Context().RepositoryStr()))
	if err != nil {
		return err
	}
	fmt.Println("fetching", resp.Request.URL)

	if err := transport.CheckError(resp, http.StatusOK); err != nil {
		return err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	fmt.Println(string(body))
	return nil
}
