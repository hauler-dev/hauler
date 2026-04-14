package content

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"

	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
)

func Load(data []byte) (schema.ObjectKind, error) {
	var tm metav1.TypeMeta
	if err := yaml.Unmarshal(data, &tm); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if tm.APIVersion == "" {
		return nil, fmt.Errorf("missing required manifest field [apiVersion]")
	}

	if tm.Kind == "" {
		return nil, fmt.Errorf("missing required manifest field [kind]")
	}

	gv := tm.GroupVersionKind().GroupVersion()
	// allow v1 content and collections
	if gv != v1.ContentGroupVersion &&
		gv != v1.CollectionGroupVersion {
		return nil, fmt.Errorf("unrecognized content or collection [%s] with [kind=%s]", tm.APIVersion, tm.Kind)
	}

	return &tm, nil
}
