package app

import (
	"os"
	"testing"
)

func Test_pkgBuildOpts_Run(t *testing.T) {
	l, _ := setupCliLogger(os.Stdout, "debug")
	tro := rootOpts{l}

	type fields struct {
		rootOpts      *rootOpts
		cfgFile       string
		name          string
		driver        string
		driverVersion string
		fleetVersion  string
		images        []string
		paths         []string
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "should package all types of local manifests",
			fields: fields{
				rootOpts:      &tro,
				cfgFile:       "pkg.yaml",
				name:          "k3s",
				driver:        "k3s",
				driverVersion: "v1.21.1+k3s1",
				fleetVersion:  "v0.3.5",
				images:        nil,
				paths: []string{
					"../../../testdata/docker-registry",
					"../../../testdata/rawmanifests",
				},
			},
			wantErr: false,
		},
		{
			name: "should package using fleet.yaml",
			fields: fields{
				rootOpts:      &tro,
				cfgFile:       "pkg.yaml",
				name:          "k3s",
				driver:        "k3s",
				driverVersion: "v1.21.1+k3s1",
				fleetVersion:  "v0.3.5",
				images:        nil,
				paths: []string{
					"../../../testdata/custom",
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &pkgBuildOpts{
				rootOpts:      tt.fields.rootOpts,
				cfgFile:       tt.fields.cfgFile,
				name:          tt.fields.name,
				driver:        tt.fields.driver,
				driverVersion: tt.fields.driverVersion,
				fleetVersion:  tt.fields.fleetVersion,
				images:        tt.fields.images,
				paths:         tt.fields.paths,
			}

			if err := o.PreRun(); err != nil {
				t.Errorf("PreRun() error = %v", err)
			}
			defer os.Remove(o.cfgFile)

			if err := o.Run(); (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
