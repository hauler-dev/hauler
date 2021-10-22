package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Content struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ContentSpec `json:"spec"`
}

type ContentSpec struct {
	Files []Fi `json:"files"`

	Images []Im `json:"images"`

	Charts []Ch `json:"charts"`
}

type Fi struct {
	Name  string `json:"name,omitempty"`
	Blobs []Blob `json:"blobs"`
}

type Blob struct {
	Name string `json:"name,omitempty"`
	Ref  string `json:"ref"`
}

type Im struct {
	Ref string `json:"ref"`
}

type Ch struct {
	// Ref is either a name in a repository
	Ref string `json:"ref"`
}
