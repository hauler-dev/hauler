package v1alpha1

import (
	"encoding/json"
	"fmt"
	"path"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Package struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec PackageSpec `json:"spec"`
}

type PackageSpec struct {
	Version   string   `json:"version"`
	Manifests []string `json:"manifests,omitempty"`
	Images    []string `json:"images,omitempty"`
	Artifacts []string `json:"artifacts,omitempty"`

	DependsOn DependsOn `json:"dependsOn,omitempty"`
}

type DependsOn struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

func (p Package) Reference(repo string) string {
	var d digest.Digest
	data, err := json.Marshal(p)
	if err != nil {
		d = digest.FromBytes([]byte(""))
	} else {
		d = digest.FromBytes(data)
	}

	name := path.Join(repo, p.Name)
	return fmt.Sprintf("%s@%s", name, d.String())
}
