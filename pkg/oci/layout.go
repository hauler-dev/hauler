package oci

import (
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
)

const refNameAnnotation = "org.opencontainers.image.ref.name"

func getIndexManifestsDescriptors(layout layout.Path) []v1.Descriptor {
	imageIndex, err := layout.ImageIndex()
	if err != nil {
		return nil
	}

	indexManifests, err := imageIndex.IndexManifest()
	if err != nil {
		return nil
	}

	return indexManifests.Manifests
}

func ListDigests(layout layout.Path) []v1.Hash {
	var digests []v1.Hash
	for _, desc := range getIndexManifestsDescriptors(layout) {
		digests = append(digests, desc.Digest)
	}
	return digests
}

func ListImages(layout layout.Path) map[string]v1.Hash {
	images := make(map[string]v1.Hash)
	for _, desc := range getIndexManifestsDescriptors(layout) {
		if image, ok := desc.Annotations[refNameAnnotation]; ok {
			images[image] = desc.Digest
		}
	}
	return images
}
