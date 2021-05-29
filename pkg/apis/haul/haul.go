package haul

import (
	"github.com/rancherfederal/hauler/pkg/apis/bundle"
	"github.com/rancherfederal/hauler/pkg/apis/driver"
	"github.com/rancherfederal/hauler/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"path/filepath"
)

type Haul struct {
	metav1.TypeMeta `yaml:",inline"`
	Metadata        metav1.ObjectMeta `yaml:"metadata,omitempty"`

	Spec HaulSpec `yaml:"spec"`
}

type HaulSpec struct {
	Driver driver.K3sDriver `yaml:"driver"`

	Bundles []bundle.Bundle `yaml:"bundles"`
}

func (h Haul) CreateLayout(root string) *util.FSLayout {
	l := util.NewLayout(root)
	l.AddDir("bin", os.ModePerm)

	for _, b := range h.Spec.Bundles {
		bundlePath := filepath.Join("bundles", b.Name)
		l.AddDir(bundlePath, os.ModePerm)
	}

	return l
}