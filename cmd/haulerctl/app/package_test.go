package app

import (
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"testing"
)

func TestPackageOptions_Preprocess(t *testing.T) {
	tmpDirName, err := ioutil.TempDir(os.TempDir(), "hauler-test-*")
	if err != nil {
		t.Fatalf("couldn't create temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDirName)

	pconfFile, err := ioutil.TempFile(tmpDirName, "package-config-*.yaml")
	if err != nil {
		t.Fatalf("couldn't create temporary package config file: %v", err)
	}

	if _, err := io.Copy(pconfFile, strings.NewReader(packageConfigStr)); err != nil {
		pconfFile.Close()
		t.Fatalf("couldn't write temporary package config file: %v", err)
	}

	pconfFile.Close()

	po := &PackageOptions{
		PackageConfigFileName: pconfFile.Name(),
		OutputFileName:        path.Join(tmpDirName, "hauler-archive.tar"),
	}

	if err := po.Preprocess(); err != nil {
		t.Errorf("unexpected error in preprocess: %v", err)
	}
}

func TestPackageOptions_Run(t *testing.T) {
	tmpDirName, err := ioutil.TempDir(os.TempDir(), "hauler-test-*")
	if err != nil {
		t.Fatalf("couldn't create temporary directory: %v", err)
	}
	defer os.RemoveAll(tmpDirName)

	pconfFile, err := ioutil.TempFile(tmpDirName, "package-config-*.yaml")
	if err != nil {
		t.Fatalf("couldn't create temporary package config file: %v", err)
	}

	if _, err := io.Copy(pconfFile, strings.NewReader(packageConfigStr)); err != nil {
		pconfFile.Close()
		t.Fatalf("couldn't write temporary package config file: %v", err)
	}

	pconfFile.Close()

	po := &PackageOptions{
		PackageConfigFileName: pconfFile.Name(),
		OutputFileName:        path.Join(tmpDirName, "hauler-archive.tar"),
	}

	if err := po.Preprocess(); err != nil {
		t.Fatalf("unexpected error in preprocess: %v", err)
	}

	if err := po.Run(); err != nil {
		t.Errorf("unexpected error in run: %v", err)
	}
}
