package archive

import (
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
)

var packageKindMap = map[v1alpha1.PackageType]string{
	v1alpha1.PackageTypeK3s:             "k3s",
	v1alpha1.PackageTypeContainerImages: "containers",
	v1alpha1.PackageTypeGitRepository:   "git",
	v1alpha1.PackageTypeFileTree:        "files",
}

var packageStringMap map[string]v1alpha1.PackageType

func init() {
	for k, v := range packageKindMap {
		packageStringMap[v] = k
	}
}
