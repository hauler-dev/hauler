package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Git struct {
	*metav1.TypeMeta  `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GitSpec `json:"spec,omitempty"`
}

type GitSpec struct {
	Git []GitRepo `json:"git,omitempty"`
}

type GitRepo struct {
	// Path is a local repository path, resolved against the manifest's own directory when relative, or a remote clone URL
	Path string `json:"path"`

	// Name optionally overrides the name derived from Path
	Name string `json:"name,omitempty"`

	// Credentials are referenced by env-var name, the same as Charts, so raw values never appear in manifests
	UsernameEnv string `json:"usernameEnv,omitempty"`
	PasswordEnv string `json:"passwordEnv,omitempty"`
	SSHKey      string `json:"sshKey,omitempty"`

	// TLS
	CertFile              string `json:"certFile,omitempty"`
	KeyFile               string `json:"keyFile,omitempty"`
	CaFile                string `json:"caFile,omitempty"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify,omitempty"`
}
