package bundle

import (
	"github.com/rancherfederal/hauler/pkg/apis/driver"
	"github.com/rancherfederal/hauler/pkg/util"
	"github.com/sirupsen/logrus"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	AutodeployDirectory = "autodeploy"
	StaticDirectory = "static"
	ImagePreloadDirectory = "images"
)

type Bundle struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`

	Images []string
}

func CreateDriverBundle(d driver.Driver) Bundle {
	return Bundle{
		Name:   d.Name(),
		Path:   "",
		Images: d.Images(),
	}
}

func (b Bundle) CreateLayout(root string) *util.FSLayout {
	l := util.NewLayout(root)
	l.AddDir(AutodeployDirectory, os.ModePerm)
	l.AddDir(StaticDirectory, os.ModePerm)
	l.AddDir(ImagePreloadDirectory, os.ModePerm)
	return l
}

//ResolveBundle will resolve all provided references
func (b *Bundle) ResolveBundleFromPath() error {
	if b.Path == "" {
		//Only need to resolve bundles that are sourced from a directory
		return nil
	}

	if _, err := os.Stat(b.Path); os.IsNotExist(err) {
		return err
	}

	manifestsPath := filepath.Join(b.Path, AutodeployDirectory)
	err := filepath.WalkDir(manifestsPath, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() { return nil }

		ext := filepath.Ext(path)
		if !(strings.HasSuffix(ext, "yaml") || strings.HasSuffix(ext, "yml")) { return nil }

		//TODO: Identify Images from manifests
		return nil
	})
	if err != nil {
		return err
	}

	imagesPath := filepath.Join(b.Path, ImagePreloadDirectory)
	err = filepath.WalkDir(imagesPath, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() { return nil }

		ext := filepath.Ext(path)
		if strings.HasSuffix(ext, "txt") {
			//Try reading as text line
			f, err := os.Open(path)
			if err != nil {
				return err
			}

			imgs, err := util.LinesToSlice(f)

			b.Images = append(b.Images, imgs...)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func (b Bundle) Setup(d driver.Driver, path string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(cwd)

	err = os.Chdir(path)
	if err != nil {
		return err
	}

	targetStatic := filepath.Join(d.VarPath(), d.AnonymousStaticPath(), b.Name)
	if err := safeSymlink(StaticDirectory, targetStatic); err != nil {
		return err
	}

	targetImages := filepath.Join(d.VarPath(), d.PreloadImagesPath(), b.Name)
	if err := safeSymlink(ImagePreloadDirectory, targetImages); err != nil {
		return err
	}

	targetManifests := filepath.Join(d.VarPath(), d.AutodeployManifestsPath(), b.Name)
	if err := safeSymlink(AutodeployDirectory, targetManifests); err != nil {
		return err
	}

	return nil
}

func safeSymlink(src, dst string) error {
	logrus.Infof("symlinking %s to %s", src, dst)
	if _, err := os.Stat(dst); err != nil {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}		
	}

	return os.Symlink(src, dst)
}