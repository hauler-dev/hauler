// Package reference provides general types to represent oci content within a registry or local oci layout
// Grammar (stolen mostly from containerd's grammar)
//
//	reference :=
package reference

import (
	"strings"

	gname "github.com/google/go-containerregistry/pkg/name"

	"hauler.dev/go/hauler/pkg/consts"
)

type Reference interface {
	// FullName is the full name of the reference
	FullName() string

	// Name is the registryless name
	Name() string
}

// NewTagged will create a new docker.NamedTagged given a path-component
func NewTagged(n string, tag string) (gname.Reference, error) {
	n = strings.Replace(strings.ToLower(n), "+", "-", -1)
	repo, err := Parse(n)
	if err != nil {
		return nil, err
	}
	tag = strings.Replace(tag, "+", "-", -1)
	return repo.Context().Tag(tag), nil
}

// Parse will parse a reference and return a name.Reference namespaced with DefaultNamespace if necessary
func Parse(ref string) (gname.Reference, error) {
	r, err := gname.ParseReference(ref, gname.WithDefaultRegistry(""), gname.WithDefaultTag(consts.DefaultTag))
	if err != nil {
		return nil, err
	}

	if !strings.ContainsRune(r.String(), '/') {
		ref = consts.DefaultNamespace + "/" + r.String()
		return gname.ParseReference(ref, gname.WithDefaultRegistry(""), gname.WithDefaultTag(consts.DefaultTag))
	}

	return r, nil
}

// Relocate returns a name.Reference given a reference and registry
func Relocate(reference string, registry string) (gname.Reference, error) {
	ref, err := gname.ParseReference(reference)
	if err != nil {
		return nil, err
	}

	relocated, err := gname.ParseReference(ref.Context().RepositoryStr(), gname.WithDefaultRegistry(registry))
	if err != nil {
		return nil, err
	}

	if _, err := gname.NewDigest(ref.Name()); err == nil {
		return relocated.Context().Digest(ref.Identifier()), nil
	}
	return relocated.Context().Tag(ref.Identifier()), nil
}
