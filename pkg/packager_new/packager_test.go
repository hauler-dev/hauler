package packager_test

import (
	. "github.com/rancherfederal/hauler/pkg/packager_new"
	"io"

	"archive/tar"
	"bytes"
	"testing"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPackager_PackageK3s_NoErrors(t *testing.T) {
	k3sPackage := v1alpha1.PackageK3s{
		Release:          "v1.20.5+k3s1",
		InstallScriptRef: "355fff3017b06cde44dbd879408a3a6826fa7125",
	}
	pkg := v1alpha1.Package{
		Name: "main",
	}
	pkg.SetK3s(k3sPackage)

	pkgConf := v1alpha1.PackageConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "hauler.cattle.io/v1alpha1",
			Kind:       "PackageConfig",
		},
		Metadata: metav1.ObjectMeta{
			Name: "test-main",
		},
		Packages: []v1alpha1.Package{
			pkg,
		},
	}

	pkgr := New(nil)
	buf := &bytes.Buffer{}

	if err := pkgr.Package(buf, pkgConf); err != nil {
		t.Fatalf("unexpected error packaging k3s artifacts: %v", err)
	}

	reader := tar.NewReader(buf)

	header, err := reader.Next()
	for err == nil {
		fileBuf := make([]byte, 0, header.Size)
		writeBuf := bytes.NewBuffer(fileBuf)
		if _, err := io.Copy(writeBuf, reader); err != nil {
			t.Errorf(
				"unexpected error reading file %s (size %d) from tar archive: %v",
				header.Name, header.Size, err,
			)
		}
		header, err = reader.Next()
	}
	if err != io.EOF {
		t.Errorf("unexpected error reading tar archive: %v", err)
	}
}
