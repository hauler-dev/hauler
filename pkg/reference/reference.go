// Package reference provides general types to represent oci content within a registry or local oci layout
// Grammar (stolen mostly from containerd's grammar)
//
// 	reference :=
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
	// FullName is the full name of the reference
	FullName() string

	// Name is the registryless name
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
	r, err := gname.ParseReference(ref, gname.WithDefaultRegistry(""), gname.WithDefaultTag(DefaultTag))
	if err != nil {
		return nil, err
	}

	if !strings.ContainsRune(r.String(), '/') {
		ref = DefaultNamespace + "/" + r.String()
		return gname.ParseReference(ref, gname.WithDefaultRegistry(""), gname.WithDefaultTag(DefaultTag))
	}

	return r, nil
}
