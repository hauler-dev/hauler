package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const FilesContentKind = "Files"

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

	// Reference is an optionally defined reference to the contents within the store
	// 	If not specified, this will be generated as follows:
	// 		hauler/<path base>:latest
	Reference string `json:"reference,omitempty"`
}
