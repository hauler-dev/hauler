package chart

import (
	"path/filepath"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

// Only a repo with both a scheme and a host is remote, so absolute and windows paths stay local.
func TestIsUrl(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: "https://charts.example.com", want: true},
		{name: "https://charts.example.com/stable/", want: true},
		{name: "http://localhost:8080", want: true},
		{name: "https://user:token@charts.example.com", want: true},
		{name: "s3://bucket/charts", want: true},
		{name: "/abs/path/testdata", want: false},
		{name: "/abs/path/testdata/", want: false},
		{name: `C:\charts`, want: false},
		{name: "C:/charts", want: false},
		{name: "testdata", want: false},
		{name: "./testdata", want: false},
		{name: "../testdata", want: false},
		{name: "", want: false},
	}
	for _, tt := range tests {
		if got := isUrl(tt.name); got != tt.want {
			t.Errorf("isUrl(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A local chart archive or directory must load from an absolute --repo path, not be sent to helm as a repo url.
func TestNewChartAbsoluteRepoPath(t *testing.T) {
	testdata, err := filepath.Abs("../../../testdata")
	if err != nil {
		t.Fatalf("resolving testdata: %v", err)
	}
	expanded := t.TempDir()
	if err := chartutil.ExpandFile(expanded, filepath.Join(testdata, "rancher-cluster-templates-0.5.2.tgz")); err != nil {
		t.Fatalf("expanding chart: %v", err)
	}

	for _, tc := range []struct{ name, repo, chart string }{
		{name: "archive", repo: testdata, chart: "rancher-cluster-templates-0.5.2.tgz"},
		{name: "archive with trailing slash", repo: testdata + string(filepath.Separator), chart: "rancher-cluster-templates-0.5.2.tgz"},
		{name: "directory", repo: expanded, chart: "rancher-cluster-templates"},
	} {
		if _, err := NewChart(tc.chart, &action.ChartPathOptions{RepoURL: tc.repo}); err != nil {
			t.Errorf("%s: NewChart with absolute repo [%s]: %v", tc.name, tc.repo, err)
		}
	}
}
