package app

import (
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"sigs.k8s.io/yaml"
	"testing"
)

func Test_createOpts_Run(t *testing.T) {
	p := v1alpha1.Package{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Package",
			APIVersion: "hauler.cattle.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1alpha1.PackageSpec{
			Fleet: v1alpha1.Fleet{Version: "0.3.5"},
			Driver: v1alpha1.Driver{
				Kind:    "k3s",
				Version: "v1.21.1+k3s1",
			},
			Paths: []string{
				"../../../testdata/docker-registry",
				"../../../testdata/rawmanifests",
			},
			Images: []string{},
		},
	}

	data, _ := yaml.Marshal(p)
	if err := ioutil.WriteFile("create_test.package.yaml", data, 0644); err != nil {
		t.Fatalf("failed to write test config file: %v", err)
	}
	defer os.Remove("create_test.package.yaml")

	type fields struct {
		driver     string
		outputFile string
		configFile string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "should work",
			fields: fields{
				driver:     "k3s",
				outputFile: "",
				configFile: "./create_test.package.yaml",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &createOpts{
				driver:     tt.fields.driver,
				outputFile: tt.fields.outputFile,
				configFile: tt.fields.configFile,
			}
			if err := o.Run(); (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
