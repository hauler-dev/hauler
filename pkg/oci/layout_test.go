package oci

import (
	"fmt"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

func Test_ListImages(t *testing.T) {

	img, err := random.Image(1024, 5)

	if err != nil {
		fmt.Printf("error creating test image: %v", err)
	}

	ly := createLayout(img, ".")
	dg := getDigest(img)

	m := ListImages(ly)

	for _, hash := range m {
		if hash != dg {
			t.Errorf("error got %v want %v", hash, dg)
		}
	}

}

func createLayout(img v1.Image, path string) layout.Path {

	p, err := layout.FromPath(path)
	if err != nil {
		fmt.Printf("error creating layout: %v", err)
	}
	p.AppendImage(img)

	return p
}

func getDigest(img v1.Image) v1.Hash {

	digest, err := img.Digest()

	if err != nil {
		fmt.Printf("error getting digest: %v", err)
	}

	return digest
}
