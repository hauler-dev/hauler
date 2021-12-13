package reference

import (
	"strings"

	gname "github.com/google/go-containerregistry/pkg/name"
)

const (
	DefaultNamespace = "hauler"
	DefaultTag       = "latest"
)

type Reference interface {
	Name() string
}

// NewTagged will create a new docker.NamedTagged given a path-component
func NewTagged(n string, tag string) (gname.Reference, error) {
	repo, err := Parse(n)
	if err != nil {
		return nil, err
	}

	return repo.Context().Tag(tag), nil
}

// Parse will parse a reference and return a name.Reference namespaced with DefaultNamespace if necessary
func Parse(ref string) (gname.Reference, error) {
	repo, err := gname.ParseReference(ref, gname.WithDefaultRegistry(""))
	if err != nil {
		return nil, err
	}

	if !strings.ContainsRune(repo.String(), '/') {
		ref = DefaultNamespace + "/" + repo.String()
	}

	r, err := gname.ParseReference(ref)
	if err != nil {
		return nil, err
	}

	return r, nil
}
