package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Ref     string          `json:"ref,omitempty"`
	Sources ImageTxtSources `json:"sources,omitempty"`
}

type ImageTxtSources struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}
