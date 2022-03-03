package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ImageTxtsContentKind = "ImageTxts"
)

type ImageTxts struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ImageTxtsSpec `json:"spec,omitempty"`
}

type ImageTxtsSpec struct {
	ImageTxts []ImageTxt `json:"imageTxts,omitempty"`
}

type ImageTxt struct {
	Path    string          `json:"path,omitempty"`
	Sources ImageTxtSources `json:"sources,omitempty"`
}

type ImageTxtSources struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}
