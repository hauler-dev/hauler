package store

import (
	"bytes"
	"fmt"
	"io"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type Proxy struct {
	Registries []Registry `json:"registries"`
}

type Registry struct {
	URL         string      `json:"url"`
	Credentials Credentials `json:"credentials"`

	// TODO:
	// Port int `json:"port"`
}

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ProxyConfig struct {
	Registries []Registry `json:"registries"`
}

func NewProxy(c ProxyConfig) *Proxy {
	return &Proxy{
		Registries: c.Registries,
	}
}

// Blob goes through the known upstream remote repos and returns the blob
func (p Proxy) GetBlob(repo name.Reference, h v1.Hash) (io.ReadCloser, error) {
	var err error
	for _, reg := range p.Registries {
		full := fmt.Sprintf("%s@%s", repo.String(), h.String())
		digest, err := name.NewDigest(full, name.WithDefaultRegistry(reg.URL))
		if err != nil {
			return nil, err
		}

		layer, err := remote.Layer(digest, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			return nil, err
		}

		if lr, err := layer.Compressed(); err == nil {
			return lr, err
		}
	}
	return nil, err
}

// TODO: Implement local storage
func (p Proxy) WriteBlob(h v1.Hash, rc io.ReadCloser) error {
	return nil
}

// TODO: Implement local storage
func (p Proxy) WriteManifest(m *v1.Manifest) error {
	return nil
}

// GetImageManifest goes through the known upstream remote repos
func (p Proxy) GetImageManifest(repo string, ref string) (v1.Descriptor, io.ReadCloser, error) {
	found := false
	d := v1.Descriptor{}
	var buf *bytes.Buffer

	for _, reg := range p.Registries {
		pref, err := ParseRepoAndReference(repo, ref, name.WithDefaultRegistry(reg.URL))
		if err != nil {
			return v1.Descriptor{}, nil, err
		}

		descriptor, err := remote.Get(pref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
		if err != nil {
			continue
		}

		found = true

		// TODO: Different logic per descriptor
		switch descriptor.MediaType {
		case types.OCIImageIndex, types.DockerManifestList:
			d = descriptor.Descriptor
			b, _ := descriptor.RawManifest()
			buf = bytes.NewBuffer(b)

		case types.DockerManifestSchema1, types.DockerManifestSchema1Signed:

		default:
			d = descriptor.Descriptor
			b, _ := descriptor.RawManifest()
			buf = bytes.NewBuffer(b)
		}
	}

	if !found {
		return d, nil, fmt.Errorf("not found")
	}

	return d, io.NopCloser(buf), nil
}
