package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	BundlesDir = "bundles"
	LayoutDir = "layout"
	BinDir = "bin"
)

type Package struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PackageSpec `json:"spec"`
}

type PackageSpec struct {
	// Paths is the list of directories relative to the working directory contains all resources to be bundled.
	// path globbing is supported, for example [ "charts/*" ] will match all folders as a subdirectory of charts/
	// If empty, "/" is the default
	Paths []string `json:"paths,omitempty"`

	Driver Driver `json:"driver,omitempty"`

	Images []string `json:"images,omitempty"`
}
