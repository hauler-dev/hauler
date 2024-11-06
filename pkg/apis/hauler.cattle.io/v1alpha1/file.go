package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Files struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec FileSpec `json:"spec,omitempty"`
}

type FileSpec struct {
	Files []File `json:"files,omitempty"`
}

type File struct {
	// Path is the path to the file contents, can be a local or remote path
	Path string `json:"path"`

	// Name is an optional field specifying the name of the file when specified,
	// 	it will override any dynamic name discovery from Path
	Name string `json:"name,omitempty"`
}
