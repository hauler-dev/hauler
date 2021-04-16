package app

import (
	"errors"

	"github.com/rancherfederal/hauler/pkg/archive"
)

type OutputFormat int

//go:generate stringer -type=OutputFormat
const (
	UnknownFormat = OutputFormat(0)
	TarGz         = OutputFormat(archive.KindTarGz)
	Tar           = OutputFormat(archive.KindTar)
)

func (i *OutputFormat) ToArchiveKind() archive.Kind {
	return archive.Kind(*i)
}

func (i *OutputFormat) Set(s string) error {
	switch s {
	case "TarGz":
		*i = TarGz
	case "Tar":
		*i = Tar
	default:
		return errors.New("unknown format")
	}
	return nil

}

func (i OutputFormat) Type() string {
	return "OutputFormat"
}
