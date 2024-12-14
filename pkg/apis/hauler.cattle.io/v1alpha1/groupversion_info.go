package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"hauler.dev/go/hauler/pkg/consts"
)

var (
	ContentGroupVersion    = schema.GroupVersion{Group: consts.ContentGroup, Version: consts.APIVersion}
	CollectionGroupVersion = schema.GroupVersion{Group: consts.CollectionGroup, Version: consts.APIVersion}
)
