package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	ContentGroup = "content.hauler.cattle.io"
)

var (
	GroupVersion = schema.GroupVersion{Group: ContentGroup, Version: "v1alpha1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
)
