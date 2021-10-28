package driver

import (
	"reflect"
	"testing"
)

func Test_newBom(t *testing.T) {
	type args struct {
		kind    string
		version string
	}
	tests := []struct {
		name    string
		args    args
		want    bom
		wantErr bool
	}{
		{
			name: "bleh",
			args: args{
				kind:    "k3s",
				version: "v1.22.2+k3s2",
			},
			want:    bom{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newBom(tt.args.kind, tt.args.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("newBom() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newBom() got = %v, want %v", got, tt.want)
			}
		})
	}
}
