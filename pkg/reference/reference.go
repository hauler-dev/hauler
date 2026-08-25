// Package reference provides general types to represent oci content within a registry or local oci layout
// Grammar (stolen mostly from containerd's grammar)
//
//	reference :=
package reference

import (
	"strings"

	dreference "github.com/distribution/reference"
	goname "github.com/google/go-containerregistry/pkg/name"

	"hauler.dev/go/hauler/v2/pkg/consts"
)

type Reference interface {
	// FullName is the full name of the reference
	FullName() string

	// Name is the registryless name
	Name() string
}

// NewTagged will create a new docker.NamedTagged given a path-component
func NewTagged(n string, tag string) (goname.Reference, error) {
	n = strings.Replace(strings.ToLower(n), "+", "-", -1)
	repo, err := Parse(n)
	if err != nil {
		return nil, err
	}
	tag = strings.Replace(tag, "+", "-", -1)
	return repo.Context().Tag(tag), nil
}

// ParseReference wraps goname.ParseReference so that a reference whose input
// carries no registry stays registryless (consts.DefaultRegistry) instead of
// defaulting to Docker Hub. Any caller-supplied options are applied after the
// default, so an explicit goname.WithDefaultRegistry still wins (options are
// applied in order).
//
// Use this when reading or constructing hauler store references, where a name
// whose registry was stripped (e.g. the AnnotationRefName annotation) must stay
// registryless rather than be mis-attributed to Docker Hub. Plain
// goname.ParseReference is still correct on the pull path, where a registryless
// source name should resolve to Docker Hub so the image can be fetched.
func ParseReference(ref string, opts ...goname.Option) (goname.Reference, error) {
	opts = append([]goname.Option{goname.WithDefaultRegistry(consts.DefaultRegistry)}, opts...)
	return goname.ParseReference(ref, opts...)
}

// Parse will parse a reference and return a name.Reference namespaced with DefaultNamespace if necessary
// for example charts stored as hauler/chart-name:tag
func Parse(ref string) (goname.Reference, error) {
	r, err := ParseReference(ref, goname.WithDefaultTag(consts.DefaultTag))
	if err != nil {
		return nil, err
	}

	if !strings.ContainsRune(r.String(), '/') {
		ref = consts.DefaultNamespace + "/" + r.String()
		return ParseReference(ref, goname.WithDefaultTag(consts.DefaultTag))
	}

	return r, nil
}

// Relocate returns a name.Reference given a reference and registry
func Relocate(reference string, registry string) (goname.Reference, error) {
	ref, err := ParseReference(reference)
	if err != nil {
		return nil, err
	}

	relocated, err := ParseReference(ref.Context().RepositoryStr(), goname.WithDefaultRegistry(registry))
	if err != nil {
		return nil, err
	}

	if _, err := goname.NewDigest(ref.Name()); err == nil {
		return relocated.Context().Digest(ref.Identifier()), nil
	}
	return relocated.Context().Tag(ref.Identifier()), nil
}

// NormalizeContainerd returns ref in the distribution-normalized form containerd
// and CRI resolve ("docker.io/library/busybox:tag", never "index.docker.io/...").
// Hauler v1 wrote store annotations this way via its cosign fork; v2's writeImage
// lost it, which is the #744 regression on the OCI import path. Unparseable input
// is returned unchanged -- never fail a flow over a name hauler itself wrote.
// Tagless input gains ":latest" (ParseDockerRef semantics); hauler always passes
// refs that already carry a tag or digest.
func NormalizeContainerd(ref string) string {
	named, err := dreference.ParseDockerRef(ref)
	if err != nil {
		return ref
	}
	return named.String()
}
