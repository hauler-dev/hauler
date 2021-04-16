package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"io"
	"path"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type writerOptions struct {
	archiveName string
}

func defaultWriterOptions() *writerOptions {
	return &writerOptions{
		archiveName: "hauler-archive",
	}
}

type WriterOption interface {
	Apply(*writerOptions)
}

type withArchiveName string

func (w *withArchiveName) Apply(o *writerOptions) {
	o.archiveName = string(*w)
}

func WithArchiveName(name string) WriterOption {
	w := withArchiveName(name)
	return &w
}

type Writer struct {
	Kind Kind

	tarWriter  *tar.Writer
	gzipWriter *gzip.Writer
}

func NewWriter(dst io.Writer, kind Kind, opts ...WriterOption) (*Writer, error) {
	opt := defaultWriterOptions()
	for _, o := range opts {
		o.Apply(opt)
	}

	w := &Writer{
		Kind: kind,
	}

	switch kind {
	case KindTar:
		w.tarWriter = tar.NewWriter(dst)
	case KindTarGz:
		w.gzipWriter = gzip.NewWriter(dst)
		w.tarWriter = tar.NewWriter(w.gzipWriter)
	default:
		return nil, fmt.Errorf("unknown writer type provided")
	}

	// add metadata file
	archiveMeta := v1alpha1.Archive{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "hauler.cattle.io/v1alpha1",
			Kind:       "Archive",
		},
		Metadata: metav1.ObjectMeta{
			Name: opt.archiveName,
		},
	}

	archiveYAML, err := yaml.Marshal(archiveMeta)
	if err != nil {
		return nil, fmt.Errorf("marshal Archive struct to YAML: %v", err)
	}

	switch kind {
	case KindTar, KindTarGz:
		archiveHeader := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     "./hauler_archive.yaml",
			Size:     int64(len(archiveYAML)),
			Mode:     0444,
		}
		if err := w.tarWriter.WriteHeader(archiveHeader); err != nil {
			return nil, fmt.Errorf("write Archive YAML header to tar archive: %v", err)
		}
		if _, err := io.Copy(w.tarWriter, bytes.NewBuffer(archiveYAML)); err != nil {
			return nil, fmt.Errorf("write Archive YAML to tar archive: %v", err)
		}
	}

	return w, nil
}

// MkdirP creates all directories required to fulfill the specified path in the
// package, mimicking the functionality of the linux mkdir -p call. Multiple
// calls to create the same path with the same packageKind and packageName will
// not create multiple copies of the same directory.
func (w *Writer) MkdirP(
	packageKind string,
	packageName string,
	path string,
) error {
	switch w.Kind {
	case KindTar, KindTarGz:
		return errors.New("unimplemented")

	default:
		return ErrNoWriterKind
	}
}

type writerFileOptions struct {
	fileMode int64
	// subdirectory string
}

func defaultWriterFileOptions() *writerFileOptions {
	return &writerFileOptions{
		fileMode: 0644,
	}
}

// WriterFileOption specifies an option passed into the CreateFile function.
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

// TODO - track which directories have been created, use MkdirP to prep and create

// CreateFile creates a new file in the destination archive, directing the next
// calls to Write to provide the contents of this file.
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

	switch w.Kind {
	case KindTar, KindTarGz:
		tarHeader := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     cleanFileName,
			Size:     fileSize,
			Mode:     opt.fileMode,
		}
		return w.tarWriter.WriteHeader(tarHeader)

	default:
		return ErrNoWriterKind
	}
}

// Write implements io.Writer
func (w *Writer) Write(b []byte) (int, error) {
	switch w.Kind {
	case KindTar, KindTarGz:
		return w.tarWriter.Write(b)

	default:
		return 0, ErrNoWriterKind
	}
}

// Close flushes and closes the underlying writers when this archive is done
// being written to.
func (w *Writer) Close() error {
	switch w.Kind {
	case KindTar:
		return w.tarWriter.Close()

	case KindTarGz:
		if err := w.tarWriter.Close(); err != nil {
			return err
		}
		return w.gzipWriter.Close()

	default:
		return ErrNoWriterKind
	}
}
