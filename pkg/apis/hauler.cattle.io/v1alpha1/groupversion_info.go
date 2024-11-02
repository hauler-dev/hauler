package v1alpha1

import (
	"hauler.dev/go/hauler/pkg/consts"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	ContentGroupVersion = schema.GroupVersion{Group: consts.ContentGroup, Version: consts.APIVersion}
	// SchemeBuilder       = &scheme.Builder{GroupVersion: ContentGroupVersion}

	CollectionGroupVersion = schema.GroupVersion{Group: consts.CollectionGroup, Version: consts.APIVersion}
)
