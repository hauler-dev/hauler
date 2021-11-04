package empty

import (
	"fmt"
	"sync"

	gv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/partial"
	gtypes "github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/rancherfederal/hauler/pkg/artifact/v1"
	"github.com/rancherfederal/hauler/pkg/artifact/v1/types"
)

var Artifact, _ = UncompressedToArtifact(emptyArtifact{})

type emptyArtifact struct {}

func (i emptyArtifact) MediaType() (types.MediaType, error) {
	return types.UnknownManifest, nil
}

// TODO
func (i emptyArtifact) RawConfigFile() ([]byte, error) {
	return []byte(""), nil
}

// TODO
func (i emptyArtifact) ConfigFile() (*gv1.ConfigFile, error) {
	return nil, nil
}

func (i emptyArtifact) LayerByDiffID(h gv1.Hash) (partial.UncompressedLayer, error) {
	return nil, fmt.Errorf("LayerByDiffID(%s): empty artifact", h)
}

type ArtifactCore interface {
	RawConfigFile() ([]byte, error)

	MediaType() (types.MediaType, error)
}

type UncompressedArtifactCore interface {
	ArtifactCore

	LayerByDiffID(gv1.Hash) (partial.UncompressedLayer, error)
}

func UncompressedToArtifact(uac UncompressedArtifactCore) (v1.Oci, error) {
	return &uncompressedArtifactExtender{
		UncompressedArtifactCore: uac,
	}, nil
}

func (u *uncompressedArtifactExtender) Layers() ([]gv1.Layer, error) {
	panic("implement me")
}

func (u *uncompressedArtifactExtender) LayerByDigest(hash gv1.Hash) (gv1.Layer, error) {
	panic("implement me")
}

func (u *uncompressedArtifactExtender) Digest() (gv1.Hash, error) {
	panic("implement me")
}

func (u *uncompressedArtifactExtender) Manifest() (*gv1.Manifest, error) {
	u.lock.Lock()
	defer u.lock.Unlock()
	if u.manifest != nil {
		return u.manifest, nil
	}

	m := &gv1.Manifest{
		SchemaVersion: 2,
		MediaType: gtypes.DockerManifestSchema2,
	}

	u.manifest = m
	return u.manifest, nil
}

func (u *uncompressedArtifactExtender) RawManifest() ([]byte, error) {
	return partial.RawManifest(u)
}

func (u *uncompressedArtifactExtender) ConfigName() (gv1.Hash, error) {
	return partial.ConfigName(u)
}

type uncompressedArtifactExtender struct {
	UncompressedArtifactCore

	lock sync.Mutex
	manifest *gv1.Manifest
}