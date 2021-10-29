package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const FilesContentKind = "Files"

type Files struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec FileSpec `json:"spec,omitempty"`
}

type FileSpec struct {
	Files []File `json:"files,omitempty"`
}

type File struct {
	Ref  string `json:"ref"`
	Name string `json:"name,omitempty"`
}
