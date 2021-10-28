package content

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
)

type Oci interface {
	// Copy relocates content to an OCI compliant registry given a name.Reference
	Copy(ctx context.Context, registry string) error
}

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

// // NewFromBytes returns a new Oci object from content bytes
// func NewFromBytes(data []byte) (Oci, error) {
// 	var tm metav1.TypeMeta
// 	if err := yaml.Unmarshal(data, &tm); err != nil {
// 		return nil, err
// 	}
//
// 	if tm.GroupVersionKind().GroupVersion() != v1alpha1.GroupVersion {
// 		return nil, fmt.Errorf("%s is not an understood content type", tm.GroupVersionKind().String())
// 	}
//
// 	var oci Oci
//
// 	switch tm.Kind {
// 	case v1alpha1.FilesContentKind:
// 		var cfg v1alpha1.Files
// 		err := yaml.Unmarshal(data, &cfg)
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		oci = file.New(cfg.Spec.Files[0].Name, cfg.Spec.Files[0].Ref)
//
// 	case v1alpha1.ImagesContentKind:
// 		var cfg v1alpha1.Images
// 		err := yaml.Unmarshal(data, &cfg)
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		oci, err = image.New(cfg.Spec.Images[0].Ref)
//
// 	case v1alpha1.ChartsContentKind:
// 		var cfg v1alpha1.Charts
// 		err := yaml.Unmarshal(data, &cfg)
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		oci = chart.New(cfg.Spec.Charts[0].Name, cfg.Spec.Charts[0].RepoURL, cfg.Spec.Charts[0].Version)
//
// 	case v1alpha1.DriverContentKind:
// 		var cfg v1alpha1.Driver
// 		err := yaml.Unmarshal(data, &cfg)
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		return nil, fmt.Errorf("%s is still a wip", tm.GroupVersionKind().String())
//
// 	default:
// 		return nil, fmt.Errorf("%s is not an understood content type", tm.GroupVersionKind().String())
// 	}
//
// 	return oci, nil
// }
//
// // NewFromFile is a helper function around NewFromBytes to load new content given a filename
// func NewFromFile(filename string) ([]Oci, error) {
// 	fi, err := os.Open(filename)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	reader := yaml.NewYAMLReader(bufio.NewReader(fi))
//
// 	var contents []Oci
// 	for {
// 		raw, err := reader.Read()
// 		if err == io.EOF {
// 			break
// 		}
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		o, err := NewFromBytes(raw)
// 		if err != nil {
// 			return nil, err
// 		}
//
// 		contents = append(contents, o)
// 	}
//
// 	return contents, err
// }
