package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Charts struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ChartSpec `json:"spec,omitempty"`
}

type ChartSpec struct {
	Charts []Chart `json:"charts,omitempty"`
}

type Chart struct {
	Name    string `json:"name,omitempty"`
	RepoURL string `json:"repoURL,omitempty"`
	Version string `json:"version,omitempty"`
	Rewrite string `json:"rewrite,omitempty"`

	AddImages       bool `json:"addImages,omitempty"`
	AddDependencies bool `json:"addDependencies,omitempty"`
}

type ThickCharts struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ThickChartSpec `json:"spec,omitempty"`
}

type ThickChartSpec struct {
	Charts []ThickChart `json:"charts,omitempty"`
}

type ThickChart struct {
	Chart       `json:",inline,omitempty"`
	ExtraImages []ChartImage `json:"extraImages,omitempty"`
}

type ChartImage struct {
	Reference string `json:"ref"`
}
