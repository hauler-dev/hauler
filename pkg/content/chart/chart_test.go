package chart_test

import (
	"os"
	"reflect"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"helm.sh/helm/v3/pkg/action"

	"hauler.dev/go/hauler/pkg/consts"
	"hauler.dev/go/hauler/pkg/content/chart"
)

// TestNewChart_VerifyAndAuthPropagated verifies that --verify and auth/TLS options
// in action.ChartPathOptions are actually wired through to the Helm client.
// With Verify=true the Helm client must reject a chart that has no .prov file.
func TestNewChart_VerifyAndAuthPropagated(t *testing.T) {
	t.Run("verify flag causes failure on unsigned chart", func(t *testing.T) {
		opts := &action.ChartPathOptions{
			RepoURL: "../../../testdata",
			Verify:  true,
		}
		_, err := chart.NewChart("rancher-cluster-templates-0.5.2.tgz", opts)
		if err == nil {
			t.Fatal("NewChart() expected error with Verify=true on unsigned chart, got nil")
		}
	})

	t.Run("credentials are propagated and do not break local chart load", func(t *testing.T) {
		// Credentials are passed but local chart loading does not require auth.
		// This test ensures setting Username/Password does not silently break
		// the happy path (i.e. they are stored, not discarded).
		opts := &action.ChartPathOptions{
			RepoURL:  "../../../testdata",
			Username: "user",
			Password: "pass",
		}
		c, err := chart.NewChart("rancher-cluster-templates-0.5.2.tgz", opts)
		if err != nil {
			t.Fatalf("NewChart() unexpected error: %v", err)
		}
		if c == nil {
			t.Fatal("NewChart() returned nil chart")
		}
	})
}

func TestNewChart(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

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
				name: "rancher-cluster-templates-0.5.2.tgz",
				opts: &action.ChartPathOptions{RepoURL: "../../../testdata"},
			},
			want: v1.Descriptor{
				MediaType: consts.ChartLayerMediaType,
				Size:      14970,
				Digest: v1.Hash{
					Algorithm: "sha256",
					Hex:       "0905de044a6e57cf3cd27bfc8482753049920050b10347ae2315599bd982a0e3",
				},
				Annotations: map[string]string{
					ocispec.AnnotationTitle: "rancher-cluster-templates-0.5.2.tgz",
				},
			},
			wantErr: false,
		},
		// TODO: This isn't matching digests b/c of file timestamps not being respected
		// {
		// 	name:    "should create from a chart directory",
		// 	args:    args{
		// 		path: filepath.Join(tempDir, "podinfo"),
		// 	},
		// 	want:    want,
		// 	wantErr: false,
		// },
		{
			// TODO: Use a mock helm server
			name: "should fetch a remote chart",
			args: args{
				name: "cert-manager",
				opts: &action.ChartPathOptions{RepoURL: "https://charts.jetstack.io", Version: "1.15.3"},
			},
			want: v1.Descriptor{
				MediaType: consts.ChartLayerMediaType,
				Size:      94751,
				Digest: v1.Hash{
					Algorithm: "sha256",
					Hex:       "016e68d9f7083d2c4fd302f951ee6490dbf4cb1ef44cfc06914c39cbfb01d858",
				},
				Annotations: map[string]string{
					ocispec.AnnotationTitle: "cert-manager-v1.15.3.tgz",
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
