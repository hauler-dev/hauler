package v1alpha1

import (
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const (
	BundlesDir = "bundles"
	LayoutDir  = "layout"
	BinDir     = "bin"
	ChartDir   = "charts"

	PackageFile = "package.json"
)

type Package struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PackageSpec `json:"spec"`
}

type PackageSpec struct {
	Fleet Fleet `json:"fleet"`

	Driver Driver `json:"driver"`

	// Paths is the list of directories relative to the working directory contains all resources to be bundled.
	// path globbing is supported, for example [ "charts/*" ] will match all folders as a subdirectory of charts/
	// If empty, "/" is the default
	Paths []string `json:"paths,omitempty"`

	Images []string `json:"images,omitempty"`
}

// LoadPackageFromDir will load an existing package from a directory on disk, it fails if no PackageFile is found in dir
func LoadPackageFromDir(path string) (Package, error) {
	data, err := os.ReadFile(filepath.Join(path, PackageFile))
	if err != nil {
		return Package{}, err
	}

	var p Package
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Package{}, err
	}

	return p, nil
}
