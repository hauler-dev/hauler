package content

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha2"
)

func Load(data []byte) (schema.ObjectKind, error) {
	var tm *metav1.TypeMeta
	if err := yaml.Unmarshal(data, &tm); err != nil {
		return nil, err
	}

	gv := tm.GroupVersionKind().GroupVersion()
	if gv != v1alpha1.ContentGroupVersion && gv != v1alpha1.CollectionGroupVersion && gv != v1alpha2.ContentGroupVersion && gv != v1alpha2.CollectionGroupVersion {
		return nil, fmt.Errorf("unrecognized API type: %s", tm.GroupVersionKind().String())
	}

	return tm, nil
}
