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

	AddImages       bool `json:"add-images,omitempty"`
	AddDependencies bool `json:"add-dependencies,omitempty"`
	ExcludeExtras   bool `json:"exclude-extras,omitempty"`

	// Verification
	Verify  bool   `json:"verify,omitempty"`
	Keyring string `json:"keyring,omitempty"`

	// Auth (HTTP repos only — for OCI registries use `hauler login`)
	Username           string `json:"username,omitempty"`
	Password           string `json:"password,omitempty"`
	PassCredentialsAll bool   `json:"passCredentialsAll,omitempty"`

	// TLS
	CertFile              string `json:"certFile,omitempty"`
	KeyFile               string `json:"keyFile,omitempty"`
	CaFile                string `json:"caFile,omitempty"`
	InsecureSkipTLSverify bool   `json:"insecureSkipTLSverify,omitempty"`
	PlainHTTP             bool   `json:"plainHTTP,omitempty"`
}
