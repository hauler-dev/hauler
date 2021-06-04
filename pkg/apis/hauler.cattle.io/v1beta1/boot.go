package v1beta1

import (
	"context"
	"fmt"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/mholt/archiver/v3"
	"github.com/otiai10/copy"
	"github.com/rancherfederal/hauler/pkg/fetcher/image"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

const (
	Name = "bundle"
	BootBundleKind = "BootBundle"
	BootBundleManifestDir = "manifests"
	BootBundleImagesDir = "oci"
	BootBundleChartsDir = "charts"
)

type BootBundle struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//TODO: Make below (de)serializable interface: Driver
	Driver K3sDriver `json:"driver,omitempty"`

	Charts []string `json:"charts,omitempty"`
	Images []string `json:"images,omitempty"`
}

func NewBootBundle(name string) *BootBundle {
	return &BootBundle{
		TypeMeta:   metav1.TypeMeta{
			APIVersion: Version,
			Kind: BootBundleKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Driver: K3sDriver{
			Type: K3sDriverName,
			Version: K3sDefaultVersion,
			Config:  K3sConfig{
				NodeName:       "hauler",
				Selinux:        false,
				KubeConfigMode: 0644,
			},
		},
	}
}

//Load will load a bundle given a directory containing a BootBundle config file
func Load(path string) (*BootBundle, error) {
	cfgPath := filepath.Join(path, fmt.Sprintf("%s.yaml", Name))
	if _, err := os.Stat(cfgPath); err != nil {
		return nil, err
	}

	data, _ := os.ReadFile(cfgPath)

	var b *BootBundle
	err := yaml.Unmarshal(data, &b)
	if err != nil {
		return nil, err
	}

	return b, err
}

func (b BootBundle) Create() error {
	logrus.Infof("creating new bundle...")

	// Create dir for bundle if it doesn't exist
	if _, err := os.Stat(b.Name); os.IsNotExist(err) {
		err := os.Mkdir(b.Name, os.ModePerm)
		if err != nil {
			return err
		}
	}

	data, err := yaml.Marshal(b)
	if err != nil {
		return err
	}

	configFilePath := filepath.Join(b.Name, fmt.Sprintf("%s.yaml", Name))
	err = os.WriteFile(configFilePath, data, os.ModePerm)
	if err != nil {
		return err
	}

	dirs := []string{BootBundleManifestDir, BootBundleChartsDir, BootBundleImagesDir}
	for _, d := range dirs {
		err := os.Mkdir(filepath.Join(b.Name, d), os.ModePerm)
		if !os.IsExist(err) && err != nil {
			return err
		}
	}

	return nil
}

func (b BootBundle) Update(ctx context.Context) error {
	imgs, err := image.NormalizeImages(b.Images)
	if err != nil {
		return err
	}

	if len(imgs) > 0 {
		logrus.Infof("saving images...")
		c, _ := image.NewClient()
		o := image.Options{}
		err := c.SaveOCI(ctx, imgs, filepath.Join(b.Name, "images"), o)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b BootBundle) Save(ctx context.Context) error {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	dirs := []string{b.imagesPath(""), b.staticPath(""), b.manifestsPath("")}
	for _, d := range dirs {
		err := os.MkdirAll(filepath.Join(tmpdir, d), os.ModePerm)
		if !os.IsExist(err) && err != nil {
			return err
		}
	}

	//Save images from layout to single tarball
	imgRefs := make(map[name.Reference]v1.Image)
	li, err := layout.ImageIndexFromPath(filepath.Join(b.Name, "images"))
	im, err := li.IndexManifest()
	for _, m := range im.Manifests {
		i, err := li.Image(m.Digest)
		if err != nil {
			return err
		}

		ref, err := name.ParseReference(m.Annotations["name"])
		if err != nil {
			return err
		}

		imgRefs[ref] = i
	}

	err = tarball.MultiRefWriteToFile(filepath.Join(b.imagesPath(tmpdir), "images.tar"), imgRefs)
	if err != nil {
		return err
	}

	//TODO: Make this more robust, right now just copy/paste pre-packaged charts
	chartsPath := filepath.Join(b.Name, "charts")
	err = copy.Copy(chartsPath, b.staticPath(tmpdir))
	if err != nil {
		return err
	}

	//TODO: Make this more robust, right now just copy/paste manifests
	manifestsPath := filepath.Join(b.Name, "manifests")
	err = copy.Copy(manifestsPath, b.manifestsPath(tmpdir))
	if err != nil {
		return err
	}

	//TODO: Centralize this
	zstd := archiver.NewTarZstd()
	zstd.OverwriteExisting = true
	err = zstd.Archive([]string{tmpdir}, fmt.Sprintf("%s.bundle.tar.zst", b.Name))
	if err != nil {
		return err
	}

	return nil
}

func (b BootBundle) imagesPath(root string) string {
	return filepath.Join(root, "agent", "images", "hauler")
}

func (b BootBundle) staticPath(root string) string {
	return filepath.Join(root, "server", "static", "hauler")
}

func (b BootBundle) manifestsPath(root string) string {
	return filepath.Join(root, "server", "manifests", "hauler")
}

func (b BootBundle) RefMap() map[name.Reference]v1.Image {
	return nil
}