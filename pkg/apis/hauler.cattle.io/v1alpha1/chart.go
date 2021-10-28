package v1alpha1

import (
	"database/sql"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ChartsContentKind = "Charts"

type Charts struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ChartSpec `json:"spec,omitempty"`
}

type ChartSpec struct {
	Charts []Chart `json:"charts,omitempty"`
}

type Chart struct {
	Name    string `json:"name"`
	RepoURL string `json:"repoURL"`
	Version string `json:"version"`

	bleh sql.ColumnType
}
