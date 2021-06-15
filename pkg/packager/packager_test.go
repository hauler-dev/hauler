package packager

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/fs"
	"testing"
)

func Test_pkg_driver(t *testing.T) {
	type fields struct {
		fs fs.PkgFs
	}
	type args struct {
		ctx context.Context
		d   v1alpha1.IDriver
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pkg{
				fs: tt.fields.fs,
			}
			if err := p.driver(tt.args.ctx, tt.args.d); (err != nil) != tt.wantErr {
				t.Errorf("driver() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
