package archive

import (
	"errors"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindTar
	KindTarGz
)

var ErrNoWriterKind = errors.New("no Kind specified for Writer")
