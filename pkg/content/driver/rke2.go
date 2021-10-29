package driver

import (
	"github.com/rancherfederal/hauler/pkg/content/file"
	"github.com/rancherfederal/hauler/pkg/content/image"
)

type Rke2 struct {
	Files  []file.File
	Images []image.Image
}
