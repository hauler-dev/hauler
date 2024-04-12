package chart_test

import (
	"os"
	"reflect"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/mholt/archiver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/action"

	"github.com/rancherfederal/hauler/pkg/consts"

	"github.com/rancherfederal/hauler/pkg/content/chart"
)

var (
	chartpath = "../../../testdata/rancher-cluster-templates-0.4.4.tgz"
)

func TestNewChart(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	if err := archiver.Unarchive(chartpath, tmpdir); err != nil {
		t.Fatal(err)
	}

	type args struct {
		name string
		opts *action.ChartPathOptions
	}
	tests := []struct {
		name    string
		args    args
		want    v1.Descriptor
		wantErr bool
	}{
		{
			name: "should create from a chart archive",
			args: args{
				name: chartpath,
				opts: &action.ChartPathOptions{},
			},
			want: v1.Descriptor{
				MediaType: consts.ChartLayerMediaType,
				Size:      13102,
				Digest: v1.Hash{
					Algorithm: "sha256",
					Hex:       "4b3bb4e474b54bf9057b298f8f11c239bb561396716d8cd5fc369c407fba2965",
				},
				Annotations: map[string]string{
					ocispec.AnnotationTitle: "rancher-cluster-templates-0.4.4.tgz",
				},
			},
			wantErr: false,
		},
		// TODO: This isn't matching digests b/c of file timestamps not being respected
		// {
		// 	name:    "should create from a chart directory",
		// 	args:    args{
		// 		path: filepath.Join(tmpdir, "podinfo"),
		// 	},
		// 	want:    want,
		// 	wantErr: false,
		// },
		{
			// TODO: Use a mock helm server
			name: "should fetch a remote chart",
			args: args{
				name: "cert-manager",
				opts: &action.ChartPathOptions{RepoURL: "https://charts.jetstack.io", Version: "1.14.4"},
			},
			want: v1.Descriptor{
				MediaType: consts.ChartLayerMediaType,
				Size:      80674,
				Digest: v1.Hash{
					Algorithm: "sha256",
					Hex:       "5775fdbc1881d6e510df76d38753af54b86bd14caa8edb28fdbb79527042dede",
				},
				Annotations: map[string]string{
					ocispec.AnnotationTitle: "cert-manager-v1.14.4.tgz",
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chart.NewChart(tt.args.name, tt.args.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLocalChart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			m, err := got.Manifest()
			if err != nil {
				t.Error(err)
			}

			// TODO: This changes when we support provenance files
			if len(m.Layers) > 1 {
				t.Errorf("Expected 1 layer for chart, got %d", len(m.Layers))
			}
			desc := m.Layers[0]

			if !reflect.DeepEqual(desc, tt.want) {
				t.Errorf("got: %v\nwant: %v", desc, tt.want)
				return
			}
		})
	}
}
