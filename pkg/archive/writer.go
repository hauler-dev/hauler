package archive

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"
)

type WriterKind int

const (
	WriterKindUnknown = iota
	WriterKindTar
)

var ErrNoKind = errors.New("no kind specified for Writer")

type writerOptions struct {
	// kind WriterKind
}

type WriterOption interface {
	Apply(*writerOptions)
}

type Writer struct {
	kind WriterKind

	tarWriter *tar.Writer
}

func NewWriter(dst io.Writer, kind WriterKind, opts ...WriterOption) (*Writer, error) {
	// opt := &writerOptions{}
	// for _, o := range opts {
	// 	o.Apply(opt)
	// }

	w := &Writer{}

	switch kind {
	case WriterKindTar:
		w.tarWriter = tar.NewWriter(dst)
	default:
		return nil, fmt.Errorf("unknown writer type provided")
	}

	return w, nil
}

// func (w *Writer) MkdirP(path string) error {
// 	switch w.kind {
// 	case WriterKindTar:
// 		return errors.New("unimplemented")
// 	default:
// 		return ErrNoKind
// 	}
// }

type writerFileOptions struct {
	fileMode int64
	// subdirectory string
}

func defaultWriterFileOptions() *writerFileOptions {
	return &writerFileOptions{
		fileMode: 0644,
	}
}

type WriterFileOption interface {
	Apply(*writerFileOptions)
}

type withFileMode int64

func (w withFileMode) Apply(o *writerFileOptions) {
	o.fileMode = int64(w)
}

func WithFileMode(mode int64) WriterFileOption {
	return withFileMode(mode)
}

func (w *Writer) CreateFile(
	packageKind string,
	packageName string,
	fileName string,
	fileSize int64,
	opts ...WriterFileOption,
) error {
	opt := defaultWriterFileOptions()
	for _, o := range opts {
		o.Apply(opt)
	}

	cleanFileName := "./" + path.Clean(path.Join(".", packageKind, packageName, fileName))

	switch w.kind {
	case WriterKindTar:
		tarHeader := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     cleanFileName,
			Size:     fileSize,
			Mode:     opt.fileMode,
		}
		return w.tarWriter.WriteHeader(tarHeader)

	default:
		return ErrNoKind
	}
}

func (w *Writer) Write(b []byte) (int, error) {
	switch w.kind {
	case WriterKindTar:
		return w.tarWriter.Write(b)

	default:
		return 0, ErrNoKind
	}
}

func (w *Writer) Close() error {
	switch w.kind {
	case WriterKindTar:
		return w.tarWriter.Close()

	default:
		return ErrNoKind
	}
}
