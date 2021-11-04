package content

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
)

const (
	UnknownLayerMediaType = "application/vnd.hauler.cattle.io.unknown"
)

func ValidateType(data []byte) (metav1.TypeMeta, error) {
	var tm metav1.TypeMeta
	if err := yaml.Unmarshal(data, &tm); err != nil {
		return metav1.TypeMeta{}, err
	}

	if tm.GroupVersionKind().GroupVersion() != v1alpha1.GroupVersion {
		return metav1.TypeMeta{}, fmt.Errorf("%s is not a registered content type", tm.GroupVersionKind().String())
	}

	return tm, nil
}
