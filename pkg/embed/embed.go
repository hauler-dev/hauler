package embed

import (
	"embed"
	"fmt"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/sirupsen/logrus"
	"io/fs"
	"os"
	"path/filepath"
)

const embedRootDir = "embedded"

//go:embed embedded/*
var e embed.FS

type embedded struct {
	fs fs.FS
	embedMap []embedMap
}

type embedMap struct {
	src string
	dst string
	mode os.FileMode
}

func RenderEmbedded(d v1alpha1.Driver) error {
	efsys, err := fs.Sub(e, embedRootDir)
	if err != nil {
		logrus.Fatalf("failed to load embedded filesystem: %v", err)
	}


	e := &embedded{
		fs:       efsys,
		embedMap: []embedMap{
			{
				src: "manifests",
				dst: filepath.Join(v1alpha1.DriverVarPath, d.String(), "server"),
				mode: 0600,
			},
			{
				src: "static",
				dst: filepath.Join(v1alpha1.DriverVarPath, d.String(), "server"),
				mode: 0600,
			},
			{
				src: "bin/k3s-init.sh",
				dst: filepath.Join("/opt", v1alpha1.HaulerBin, "k3s-init.sh"),
				mode: 0755,
			},
		},
	}

	if err := e.render(); err != nil {
		return err
	}

	return nil
}

func (e *embedded) render() error {
	for _, em := range e.embedMap {
		f, err := fs.Stat(e.fs, em.src)
		if err != nil {
			return err
		}

		if f.IsDir() {
			err := e.WalkDir(em.src, em.dst, em.mode)
			if err != nil {
				return err
			}
		} else {
			err := e.WriteTo(em.src, em.dst, em.mode)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *embedded) WriteTo(src string, dest string, mode os.FileMode) error {
	logrus.Infof("creating file from embedded store: %s", src)

	data, err := fs.ReadFile(e.fs, src)
	if err != nil {
		return fmt.Errorf("failed to read file %s from embedded store: %v", src, err)
	}

	err = os.WriteFile(dest, data, mode)
	if err != nil {
		return fmt.Errorf("failed to write %s to %s from embedded store: %v", src, dest, err)
	}

	return nil
}

func (e *embedded) WalkDir(src string, dest string, mode os.FileMode) error {
	err := fs.WalkDir(e.fs, src, func(path string, d fs.DirEntry, err error) error {
		p := filepath.Join(dest, path)
		if d.IsDir() {
			logrus.Infof("creating directory: %s", p)
			err := os.Mkdir(filepath.Join(dest, path), mode)

			//TODO: lol
			if err == fs.ErrExist {
				return fmt.Errorf("failed to create directory: %v", err)
			}
		} else {
			err := e.WriteTo(path, p, mode)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}