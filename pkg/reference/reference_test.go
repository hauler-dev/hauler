package reference_test

import (
	"reflect"
	"testing"

	"hauler.dev/go/hauler/v2/pkg/reference"
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

func TestNormalizeContainerd(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"index.docker.io digest ref", "index.docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0", "docker.io/library/busybox@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0"},
		{"index.docker.io tag ref", "index.docker.io/library/nginx:1.25", "docker.io/library/nginx:1.25"},
		{"short docker hub ref", "busybox:latest", "docker.io/library/busybox:latest"},
		{"non-hub registry untouched", "ghcr.io/org/img:v1", "ghcr.io/org/img:v1"},
		{"port registry untouched", "localhost:5000/test/img:v1", "localhost:5000/test/img:v1"},
		{"unparseable returns input", "not a ref!!", "not a ref!!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reference.NormalizeContainerd(tc.in); got != tc.want {
				t.Errorf("NormalizeContainerd(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
