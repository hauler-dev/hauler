package oci

import (
	"fmt"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"os"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

func Test_ListImages(t *testing.T) {
	tmpdir, err := os.MkdirTemp(".", "hauler")
	if err != nil {
		t.Errorf("failed to setup test scaffolding: %v", err)
	}
	defer os.RemoveAll(tmpdir)

	img, err := random.Image(1024, 5)

	if err != nil {
		fmt.Printf("error creating test image: %v", err)
	}

	ly, err := createLayout(img, tmpdir)
	if err != nil {
		t.Errorf("%v", err)
	}

	dg, err := getDigest(img)
	if err != nil {
		t.Errorf("%v", err)
	}

	m := ListImages(ly)

	for _, hash := range m {
		if hash != dg {
			t.Errorf("error got %v want %v", hash, dg)
		}
	}

}

func createLayout(img v1.Image, path string) (layout.Path, error) {
	p, err := layout.FromPath(path)
	if os.IsNotExist(err) {
		p, err = layout.Write(path, empty.Index)
		if err != nil {
			return "", err
		}
	}

	if err != nil {
		return "", fmt.Errorf("error creating layout: %v", err)
	}
	if err := p.AppendImage(img); err != nil {
		return "", err
	}

	return p, nil
}

func getDigest(img v1.Image) (v1.Hash, error) {
	digest, err := img.Digest()

	if err != nil {
		return v1.Hash{}, fmt.Errorf("error getting digest: %v", err)
	}

	return digest, nil
}
