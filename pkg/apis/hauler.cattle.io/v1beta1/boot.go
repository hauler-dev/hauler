package v1beta1

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

const (
	BootBundleKind = "BootBundle"
	BootBundleManifestDir = "manifests"
	BootBundleImagesDir = "images"
	BootBundleChartsDir = "charts"
)

type BootBundle struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

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
	}
}

func (b BootBundle) Save() error {
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

	configFilePath := filepath.Join(b.Name, fmt.Sprintf("%s.yaml", b.Name))
	err = os.WriteFile(configFilePath, data, os.ModePerm)
	if err != nil {
		return err
	}

	dirs := []string{BootBundleManifestDir, BootBundleImagesDir, BootBundleChartsDir}
	for _, d := range dirs {
		err := os.Mkdir(filepath.Join(b.Name, d), os.ModePerm)
		if err != nil {
			return err
		}
	}

	return nil
}
