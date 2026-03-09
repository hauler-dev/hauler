package v1alpha1

import (
	"fmt"

	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	v1alpha1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
)

// converts v1alpha1.Files -> v1.Files
func ConvertFiles(in *v1alpha1.Files, out *v1.Files) error {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec.Files = make([]v1.File, len(in.Spec.Files))
	for i := range in.Spec.Files {
		out.Spec.Files[i].Name = in.Spec.Files[i].Name
		out.Spec.Files[i].Path = in.Spec.Files[i].Path
	}
	return nil
}

// converts v1alpha1.Images -> v1.Images
func ConvertImages(in *v1alpha1.Images, out *v1.Images) error {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec.Images = make([]v1.Image, len(in.Spec.Images))
	for i := range in.Spec.Images {
		out.Spec.Images[i].Name = in.Spec.Images[i].Name
		out.Spec.Images[i].Platform = in.Spec.Images[i].Platform
		out.Spec.Images[i].Key = in.Spec.Images[i].Key
	}
	return nil
}

// converts v1alpha1.Charts -> v1.Charts
func ConvertCharts(in *v1alpha1.Charts, out *v1.Charts) error {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec.Charts = make([]v1.Chart, len(in.Spec.Charts))
	for i := range in.Spec.Charts {
		out.Spec.Charts[i].Name = in.Spec.Charts[i].Name
		out.Spec.Charts[i].RepoURL = in.Spec.Charts[i].RepoURL
		out.Spec.Charts[i].Version = in.Spec.Charts[i].Version
	}
	return nil
}

// convert v1alpha1 object to v1 object
func ConvertObject(in interface{}) (interface{}, error) {
	switch src := in.(type) {

	case *v1alpha1.Files:
		dst := &v1.Files{}
		if err := ConvertFiles(src, dst); err != nil {
			return nil, err
		}
		return dst, nil

	case *v1alpha1.Images:
		dst := &v1.Images{}
		if err := ConvertImages(src, dst); err != nil {
			return nil, err
		}
		return dst, nil

	case *v1alpha1.Charts:
		dst := &v1.Charts{}
		if err := ConvertCharts(src, dst); err != nil {
			return nil, err
		}
		return dst, nil

	}

	return nil, fmt.Errorf("unsupported object type [%T]", in)
}
