package repodata

import "testing"

func TestPackageType(t *testing.T) {
	for name, want := range map[string]string{
		"hello-1.0-1.x86_64.rpm":     "rpm",
		"HELLO-1.0-1.NOARCH.RPM":     "rpm",
		"el9/hello-1.0-1.noarch.rpm": "rpm",
		"hello_1.0-1_amd64.deb":      "deb",
		"hello.deb.txt":              "",
		"install.sh":                 "",
		"rpm":                        "",
		"":                           "",
	} {
		if got := PackageType(name); got != want {
			t.Errorf("PackageType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestPackagePath(t *testing.T) {
	tests := []struct {
		title, dest, repo string
		wantErr           bool
	}{
		{title: "jq.rpm", dest: "rpms/jq.rpm", repo: "rpms"},
		{title: "el9/jq.rpm", dest: "rpms/el9/jq.rpm", repo: "rpms/el9"},
		{title: "el9/x86_64/jq.rpm", dest: "rpms/el9/x86_64/jq.rpm", repo: "rpms/el9/x86_64"},
		{title: "./el9//jq.rpm", dest: "rpms/el9/jq.rpm", repo: "rpms/el9"},
		{title: "jq.deb", dest: "debs/jq.deb", repo: DefaultDebSuite},
		{title: "bookworm/jq.deb", dest: "debs/bookworm/jq.deb", repo: "bookworm"},
		{title: "ubuntu/jammy/jq.deb", dest: "debs/ubuntu/jammy/jq.deb", repo: "ubuntu/jammy"},
		{title: "el9/dists/jq.rpm", dest: "rpms/el9/dists/jq.rpm", repo: "rpms/el9/dists"},
		{title: "repodata/jq.rpm", wantErr: true},
		{title: "el9/repodata/jq.rpm", wantErr: true},
		{title: "dists/jq.deb", wantErr: true},
		{title: "../jq.rpm", wantErr: true},
		{title: "/tmp/jq.deb", wantErr: true},
		{title: "notes.txt", wantErr: true},
	}
	for _, tt := range tests {
		dest, repo, err := PackagePath(tt.title)
		if (err != nil) != tt.wantErr || dest != tt.dest || repo != tt.repo {
			t.Errorf("PackagePath(%q) = %q, %q, %v... want %q, %q, error %v", tt.title, dest, repo, err, tt.dest, tt.repo, tt.wantErr)
		}
	}
}
