package reference_test

import (
	"reflect"
	"testing"

	goname "github.com/google/go-containerregistry/pkg/name"

	"hauler.dev/go/hauler/v2/pkg/reference"
)

// TestParseReference pins the two behaviors ParseReference's doc promises:
//  1. a registryless input stays registryless (empty registry, no Docker Hub
//     "library/" namespacing) instead of being defaulted to index.docker.io, so
//     a ghcr/quay image whose registry was stripped into AnnotationRefName is
//     never mis-attributed to Docker Hub on read-back; and
//  2. a caller-supplied WithDefaultRegistry still wins, since the default is
//     prepended and options apply in order.
func TestParseReference(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		wantName     string
		wantRegistry string
		wantErr      bool
	}{
		{
			name:         "registryless single-segment stays registryless without library/",
			ref:          "nginx:latest",
			wantName:     "nginx:latest",
			wantRegistry: "",
		},
		{
			name:         "registryless namespaced ref is not defaulted to docker hub",
			ref:          "org/img:v1",
			wantName:     "org/img:v1",
			wantRegistry: "",
		},
		{
			name:         "explicit registry is preserved",
			ref:          "ghcr.io/org/img:v1",
			wantName:     "ghcr.io/org/img:v1",
			wantRegistry: "ghcr.io",
		},
		{
			name:         "registry with port is preserved",
			ref:          "localhost:5000/team/app:dev",
			wantName:     "localhost:5000/team/app:dev",
			wantRegistry: "localhost:5000",
		},
		{
			name:         "fully-qualified digest ref is preserved",
			ref:          "index.docker.io/library/nginx@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0",
			wantName:     "index.docker.io/library/nginx@sha256:498a000f370d8c37927118ed80afe8adc38d1edcbfc071627d17b25c88efcab0",
			wantRegistry: "index.docker.io",
		},
		{
			name:    "unparseable ref errors",
			ref:     "NOT A REF!!",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reference.ParseReference(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseReference(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Name() != tt.wantName {
				t.Errorf("ParseReference(%q).Name() = %q, want %q", tt.ref, got.Name(), tt.wantName)
			}
			if reg := got.Context().RegistryStr(); reg != tt.wantRegistry {
				t.Errorf("ParseReference(%q) registry = %q, want %q", tt.ref, reg, tt.wantRegistry)
			}
		})
	}
}

// TestParseReferenceOverrideWins verifies a caller-supplied WithDefaultRegistry
// takes precedence over the prepended registryless default.
func TestParseReferenceOverrideWins(t *testing.T) {
	got, err := reference.ParseReference("nginx:latest", goname.WithDefaultRegistry("example.com"))
	if err != nil {
		t.Fatalf("ParseReference() error = %v", err)
	}
	if reg := got.Context().RegistryStr(); reg != "example.com" {
		t.Errorf("registry = %q, want %q", reg, "example.com")
	}
	if got.Name() != "example.com/nginx:latest" {
		t.Errorf("Name() = %q, want %q", got.Name(), "example.com/nginx:latest")
	}
}

// TestParseReferenceRegistrylessVsGoname documents why the store read-back path
// uses this wrapper rather than plain goname.ParseReference: goname defaults a
// registryless ref to Docker Hub (adding index.docker.io and, for single-segment
// repos, the library/ prefix), which would corrupt a stripped store reference.
// The pull path still uses goname deliberately, precisely because it wants that
// Docker Hub defaulting; this test guards only the read-back invariant.
func TestParseReferenceRegistrylessVsGoname(t *testing.T) {
	const ref = "nginx:latest"

	hauler, err := reference.ParseReference(ref)
	if err != nil {
		t.Fatalf("reference.ParseReference() error = %v", err)
	}
	hub, err := goname.ParseReference(ref)
	if err != nil {
		t.Fatalf("goname.ParseReference() error = %v", err)
	}

	if hauler.Context().RegistryStr() != "" {
		t.Errorf("hauler registry = %q, want empty (registryless)", hauler.Context().RegistryStr())
	}
	if hub.Context().RegistryStr() != "index.docker.io" {
		t.Errorf("goname registry = %q, want index.docker.io (baseline assumption)", hub.Context().RegistryStr())
	}
	if hauler.Name() == hub.Name() {
		t.Errorf("expected wrapper to stay registryless while goname defaults to Docker Hub, both = %q", hauler.Name())
	}
}

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
