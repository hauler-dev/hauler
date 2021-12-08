package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	Version         = "v1alpha1"
	ContentGroup    = "content.hauler.cattle.io"
	CollectionGroup = "collection.hauler.cattle.io"
)

var (
	ContentGroupVersion = schema.GroupVersion{Group: ContentGroup, Version: Version}
	// SchemeBuilder       = &scheme.Builder{GroupVersion: ContentGroupVersion}

	CollectionGroupVersion = schema.GroupVersion{Group: CollectionGroup, Version: Version}
)
