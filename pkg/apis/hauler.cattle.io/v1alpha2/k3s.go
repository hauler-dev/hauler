package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const K3sCollectionKind = "K3s"

type K3s struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec K3sSpec `json:"spec,omitempty"`
}

type K3sSpec struct {
	Version string `json:"version"`
	Arch    string `json:"arch"`
}
