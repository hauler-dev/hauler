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

// converts v1alpha1.ThickCharts -> v1.ThickCharts
func ConvertThickCharts(in *v1alpha1.ThickCharts, out *v1.ThickCharts) error {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec.Charts = make([]v1.ThickChart, len(in.Spec.Charts))
	for i := range in.Spec.Charts {
		out.Spec.Charts[i].Chart.Name = in.Spec.Charts[i].Chart.Name
		out.Spec.Charts[i].Chart.RepoURL = in.Spec.Charts[i].Chart.RepoURL
		out.Spec.Charts[i].Chart.Version = in.Spec.Charts[i].Chart.Version
	}
	return nil
}

// converts v1alpha1.ImageTxts -> v1.ImageTxts
func ConvertImageTxts(in *v1alpha1.ImageTxts, out *v1.ImageTxts) error {
	out.TypeMeta = in.TypeMeta
	out.ObjectMeta = in.ObjectMeta
	out.Spec.ImageTxts = make([]v1.ImageTxt, len(in.Spec.ImageTxts))
	for i := range in.Spec.ImageTxts {
		out.Spec.ImageTxts[i].Ref = in.Spec.ImageTxts[i].Ref
		out.Spec.ImageTxts[i].Sources.Include = append(
			out.Spec.ImageTxts[i].Sources.Include,
			in.Spec.ImageTxts[i].Sources.Include...,
		)
		out.Spec.ImageTxts[i].Sources.Exclude = append(
			out.Spec.ImageTxts[i].Sources.Exclude,
			in.Spec.ImageTxts[i].Sources.Exclude...,
		)
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

	case *v1alpha1.ThickCharts:
		dst := &v1.ThickCharts{}
		if err := ConvertThickCharts(src, dst); err != nil {
			return nil, err
		}
		return dst, nil

	case *v1alpha1.ImageTxts:
		dst := &v1.ImageTxts{}
		if err := ConvertImageTxts(src, dst); err != nil {
			return nil, err
		}
		return dst, nil
	}

	return nil, fmt.Errorf("unsupported object type [%T]", in)
}
