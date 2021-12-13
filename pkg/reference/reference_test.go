package reference_test

import (
	"reflect"
	"testing"

	"github.com/rancherfederal/hauler/pkg/reference"
)

func TestParse(t *testing.T) {
	type args struct {
		ref string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "Should add hauler namespace when doesn't exist",
			args: args{
				ref: "myfile",
			},
			want:    "hauler/myfile:latest",
			wantErr: false,
		},
		{
			name: "shouldn't modify namespaced reference",
			args: args{
				ref: "rancher/rancher:latest",
			},
			want:    "rancher/rancher:latest",
			wantErr: false,
		},
		{
			name: "Shouldn't modify canonical reference",
			args: args{
				ref: "index.docker.io/library/registry@sha256:42043edfae481178f07aa077fa872fcc242e276d302f4ac2026d9d2eb65b955f",
			},
			want:    "index.docker.io/library/registry@sha256:42043edfae481178f07aa077fa872fcc242e276d302f4ac2026d9d2eb65b955f",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reference.Parse(tt.args.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Name(), tt.want) {
				t.Errorf("Parse() got = %v, want %v", got, tt.want)
			}
		})
	}
}
