package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DriverContentKind = "Driver"
)

type Driver struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DriverSpec `json:"spec"`
}

type DriverSpec struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}
