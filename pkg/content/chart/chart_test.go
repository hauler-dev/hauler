package chart_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/mholt/archiver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/rancherfederal/hauler/pkg/consts"
	"github.com/rancherfederal/hauler/pkg/content/chart"
)

var (
	chartpath = "../../../testdata/podinfo-6.0.3.tgz"
)

func TestNewLocalChart(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	if err := archiver.Unarchive(chartpath, tmpdir); err != nil {
		t.Fatal(err)
	}

	want := v1.Descriptor{
		MediaType: consts.ChartLayerMediaType,
		Size:      13524,
		Digest: v1.Hash{
			Algorithm: "sha256",
			Hex:       "e30b95a08787de69ffdad3c232d65cfb131b5b50c6fd44295f48a078fceaa44e",
		},
		Annotations: map[string]string{
			ocispec.AnnotationTitle: filepath.Base(chartpath),
		},
	}

	type args struct {
		path string
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
				path: chartpath,
			},
			want:    want,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chart.NewLocalChart(tt.args.path)
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

			if !reflect.DeepEqual(desc, want) {
				t.Errorf("%v | %v", desc, want)
				return
			}
		})
	}
}
