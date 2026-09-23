package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Directories struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DirectorySpec `json:"spec,omitempty"`
}

type DirectorySpec struct {
	Directories []Directory `json:"directories,omitempty"`
}

type Directory struct {
	// Path is the local path to the directory, relative paths resolve against the manifest's own directory
	Path string `json:"path"`

	// Name optionally overrides the name derived from Path
	Name string `json:"name,omitempty"`
}
