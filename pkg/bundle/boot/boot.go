package boot

import (
	"context"
	"fmt"
	"github.com/rancherfederal/hauler/pkg/bundle/image"
	"github.com/rancherfederal/hauler/pkg/util"
	"net/http"
	"net/url"
	"path/filepath"
)

const (
	bootCfgFile = "boot.bundle.json"
)

type Bundle struct {
	Name string `json:"name"`
	Images []string `json:"images,omitempty"`
	Charts []string `json:"charts,omitempty"`

	Driver K3sDriver `json:"driver"`

	imageBundle *image.Bundle
}

func (b Bundle) GetName() string { return b.Name }

func (b Bundle) Sync(ctx context.Context, path string) error {
	driverImages, err := b.Driver.Images()
	if err != nil {
		return err
	}

	bundleImages := append(driverImages, b.Images...)

	//TODO: There's a better way to do this
	b.imageBundle = &image.Bundle{ Images: bundleImages }

	err = b.imageBundle.Sync(ctx, filepath.Join(path, "images"))
	if err != nil {
		return err
	}

	return nil
}

//Relocate will move all the necessary driver artifacts into their appropriate places on the host system
func (b Bundle) Install(src string) error {
	return nil
}

type K3sDriver struct {
	Version string
}

func (k K3sDriver) Name() string { return "k3s" }
func (k K3sDriver) Images() ([]string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s-images.txt", "https://github.com/k3s-io/k3s/releases/download", k.Version, k.Name()))
	if err != nil {
		return nil, fmt.Errorf("error building k3s url")
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return util.LinesToSlice(resp.Body)
}