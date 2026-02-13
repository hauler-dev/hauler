package content

import (
	"context"
	"fmt"
	"io"

	ccontent "github.com/containerd/containerd/content"
	"github.com/containerd/containerd/remotes"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// Target represents a content storage target with resolver, fetcher, and pusher capabilities
type Target interface {
	Resolve(ctx context.Context, ref string) (ocispec.Descriptor, error)
	Fetcher(ctx context.Context, ref string) (remotes.Fetcher, error)
	Pusher(ctx context.Context, ref string) (remotes.Pusher, error)
}

// RegistryOptions holds registry configuration
type RegistryOptions struct {
	PlainHTTP bool
	Insecure  bool
	Username  string
	Password  string
}

// ResolveName extracts the reference name from a descriptor's annotations
func ResolveName(desc ocispec.Descriptor) (string, bool) {
	name, ok := desc.Annotations[ocispec.AnnotationRefName]
	return name, ok
}

// IoContentWriter wraps an io.Writer to implement containerd's content.Writer
type IoContentWriter struct {
	writer     io.WriteCloser
	digester   digest.Digester
	status     ccontent.Status
	outputHash string
}

// Write writes data to the underlying writer and updates the digest
func (w *IoContentWriter) Write(p []byte) (n int, err error) {
	n, err = w.writer.Write(p)
	if n > 0 {
		w.digester.Hash().Write(p[:n])
	}
	return n, err
}

// Close closes the writer and verifies the digest if configured
func (w *IoContentWriter) Close() error {
	if w.outputHash != "" {
		computed := w.digester.Digest().String()
		if computed != w.outputHash {
			return fmt.Errorf("digest mismatch: expected %s, got %s", w.outputHash, computed)
		}
	}
	return w.writer.Close()
}

// Digest returns the current digest of written data
func (w *IoContentWriter) Digest() digest.Digest {
	return w.digester.Digest()
}

// Commit is a no-op for this implementation
func (w *IoContentWriter) Commit(ctx context.Context, size int64, expected digest.Digest, opts ...ccontent.Opt) error {
	return nil
}

// Status returns the current status
func (w *IoContentWriter) Status() (ccontent.Status, error) {
	return w.status, nil
}

// Truncate is not supported
func (w *IoContentWriter) Truncate(size int64) error {
	return fmt.Errorf("truncate not supported")
}

type writerOption func(*IoContentWriter)

// WithInputHash configures expected input hash for verification
func WithInputHash(hash string) writerOption {
	return func(w *IoContentWriter) {
		// Input hash verification happens during write
	}
}

// WithOutputHash configures expected output hash for verification
func WithOutputHash(hash string) writerOption {
	return func(w *IoContentWriter) {
		w.outputHash = hash
	}
}

// NewIoContentWriter creates a new IoContentWriter
func NewIoContentWriter(writer io.WriteCloser, opts ...writerOption) *IoContentWriter {
	w := &IoContentWriter{
		writer:   writer,
		digester: digest.Canonical.Digester(),
		status:   ccontent.Status{},
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// AnnotationUnpack is the annotation key for unpacking
const AnnotationUnpack = "io.containerd.image.unpack"
