package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ImagesContentKind = "Images"

type Images struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImageSpec `json:"spec,omitempty"`
}

type ImageSpec struct {
	Images []Image `json:"images,omitempty"`
}

type Image struct {
	Name string `json:"name"`
}
