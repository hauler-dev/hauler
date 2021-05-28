package haul

import (
	"github.com/rancherfederal/hauler/pkg/apis/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Haul struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	Metadata metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec HaulSpec `json:"spec" yaml:"spec"`
}

type HaulSpec struct {
	Driver driver.Driver

	PreloadImages []string `json:"preloadedImages" yaml:"preloadedImages"`
}
