package getter

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/consts"
)

// HttpOptions configures the behaviour of the Http getter.
type HttpOptions struct {
	// AllowInternalTargets disables the default SSRF guard that rejects
	// requests whose resolved IP falls in RFC-1918, loopback, link-local, or
	// unique-local space.  Set to true only for isolated internal CI
	// environments that intentionally fetch from private hosts.
	AllowInternalTargets bool

	// Timeout overrides the default HTTP client timeout.
	Timeout time.Duration

	// MaxBytes overrides the default per-response download cap.  Zero means
	// use consts.MaxDownloadBytes.
	MaxBytes int64
}

// Http is the Getter for http/https URLs.
type Http struct {
	opts     HttpOptions
	client   *http.Client
	maxBytes int64
}

func NewHttp() *Http {
	return NewHttpWithOptions(HttpOptions{})
}

func NewHttpWithTimeout(d time.Duration) *Http {
	return NewHttpWithOptions(HttpOptions{Timeout: d})
}

func NewHttpWithOptions(opts HttpOptions) *Http {
	timeout := time.Duration(consts.HTTPClientTimeout) * time.Second
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = consts.MaxDownloadBytes
	}

	h := &Http{opts: opts, maxBytes: maxBytes}
	h.client = &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return h.validateRequest(req)
		},
	}
	return h
}

// validateRequest enforces scheme and (when AllowInternalTargets is false)
// private-IP restrictions.  It is called for the initial request and each
// redirect hop via CheckRedirect.
func (h *Http) validateRequest(req *http.Request) error {
	switch req.URL.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("scheme %q is not allowed; only http and https are permitted", req.URL.Scheme)
	}

	if h.opts.AllowInternalTargets {
		return nil
	}

	host := req.URL.Hostname()
	addrs, err := net.LookupHost(host)
	if err != nil {
		// If we cannot resolve, let the transport fail naturally.
		return nil
	}
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		if isInternalIP(ip) {
			return fmt.Errorf("request to %s rejected: resolved to internal address %s (use --allow-internal-targets to override)", host, addr)
		}
	}
	return nil
}

// isInternalIP reports whether ip is in a private, loopback, link-local, or
// unique-local range.
func isInternalIP(ip net.IP) bool {
	private := []net.IPNet{
		// IPv4 loopback
		{IP: net.IP{127, 0, 0, 0}, Mask: net.CIDRMask(8, 32)},
		// RFC-1918 private ranges
		{IP: net.IP{10, 0, 0, 0}, Mask: net.CIDRMask(8, 32)},
		{IP: net.IP{172, 16, 0, 0}, Mask: net.CIDRMask(12, 32)},
		{IP: net.IP{192, 168, 0, 0}, Mask: net.CIDRMask(16, 32)},
		// IPv4 link-local
		{IP: net.IP{169, 254, 0, 0}, Mask: net.CIDRMask(16, 32)},
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for i := range private {
		if private[i].Contains(ip) {
			return true
		}
	}
	// IPv6 unique-local (fc00::/7)
	if len(ip) == 16 && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

func (h Http) Name(u *url.URL) string {
	req, err := http.NewRequest(http.MethodHead, u.String(), nil)
	if err != nil {
		return ""
	}
	if err := h.validateRequest(req); err != nil {
		return ""
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	unescaped, err := url.PathUnescape(u.String())
	if err != nil {
		return ""
	}

	contentType := resp.Header.Get("Content-Type")
	for _, v := range strings.Split(contentType, ",") {
		t, _, err := mime.ParseMediaType(v)
		if err != nil {
			break
		}
		// TODO: Identify known mimetypes for hints at a filename
		_ = t
	}

	return filepath.Base(unescaped)
}

func (h Http) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := h.validateRequest(req); err != nil {
		return nil, err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status fetching %s: %s", u.String(), resp.Status)
	}

	// Validate Content-Length header if the server provided one.
	if resp.ContentLength > h.maxBytes {
		resp.Body.Close()
		return nil, fmt.Errorf("remote file at %s exceeds maximum allowed size (%d bytes)", u.String(), h.maxBytes)
	}

	// Wrap the body so a server that lies about Content-Length (or omits it)
	// is still bounded.  Read maxBytes+1 so we can distinguish "exactly at cap"
	// from "exceeded cap".
	limited := &limitedReadCloser{
		r:   io.LimitReader(resp.Body, h.maxBytes+1),
		c:   resp.Body,
		max: h.maxBytes,
	}
	return limited, nil
}

func (h Http) Detect(u *url.URL) bool {
	switch u.Scheme {
	case "http", "https":
		return true
	}
	return false
}

func (h *Http) Config(u *url.URL) artifacts.Config {
	c := &httpConfig{
		config{Reference: u.String()},
	}
	return artifacts.ToConfig(c, artifacts.WithConfigMediaType(consts.FileHttpConfigMediaType))
}

// limitedReadCloser wraps an io.LimitReader and returns an error when the cap
// is hit rather than silently returning EOF.
type limitedReadCloser struct {
	r    io.Reader
	c    io.Closer
	max  int64
	read int64
}

func (l *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.read += int64(n)
	if l.read > l.max {
		return n, fmt.Errorf("download exceeded maximum allowed size of %d bytes", l.max)
	}
	return n, err
}

func (l *limitedReadCloser) Close() error {
	return l.c.Close()
}

type httpConfig struct {
	config `json:",inline,omitempty"`
}
