package content

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/hauler-dev/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
)

func Load(data []byte) (schema.ObjectKind, error) {
	var tm metav1.TypeMeta
	if err := yaml.Unmarshal(data, &tm); err != nil {
		return nil, err
	}

	if tm.GroupVersionKind().GroupVersion() != v1alpha1.ContentGroupVersion && tm.GroupVersionKind().GroupVersion() != v1alpha1.CollectionGroupVersion {
		return nil, fmt.Errorf("unrecognized content/collection type: %s", tm.GroupVersionKind().String())
	}

	return &tm, nil
}
