package bootstrap

import (
	"context"
	"github.com/rancherfederal/hauler/pkg/apis/haul"
	"github.com/rancherfederal/hauler/pkg/archive"
	"os"
)

type bootstrapper struct {
	haul haul.Haul
}

func NewBootstrapper(haulPath string) *bootstrapper {
	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return nil
	}
	defer os.Remove(tmpdir)

	a := archive.NewArchiver()
	err = a.Unarchive(haulPath, tmpdir)

	var h haul.Haul

	return &bootstrapper{
		haul: h,
	}
}

func (b bootstrapper) Bootstrap(ctx context.Context) error {
	a := archive.NewArchiver()
	_ = a

	tmpdir, err := os.MkdirTemp("", "hauler")
	if err != nil {
		return err
	}
	defer os.Remove(tmpdir)

	err = a.Unarchive(".", tmpdir)
	if err != nil {
		return err
	}

	return nil
}