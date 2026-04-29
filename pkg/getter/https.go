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

// dialTimeout is the TCP connect and keep-alive timeout used by safeDial.
const dialTimeout = 30 * time.Second

// HttpOptions configures the behaviour of the Http getter.
type HttpOptions struct {
	// AllowInternalTargets disables the SSRF guard that is enforced at dial
	// time by the custom DialContext.  When false (the default), every IP
	// address returned by DNS resolution is validated against isInternalIP
	// before any connection is attempted, and the connection is made to the
	// resolved IP literal directly so the check and the connect target the
	// same address.  Set to true only for isolated internal CI environments
	// that intentionally fetch from private or loopback hosts.
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

	baseDialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: dialTimeout,
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return h.safeDial(ctx, baseDialer, network, address)
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		// Do NOT set TLSClientConfig: Go derives tls.Config.ServerName from
		// the request URL hostname, so TLS cert verification continues to use
		// the hostname even though we dial by IP literal.
	}

	h.client = &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return h.validateRequest(req)
		},
	}
	return h
}

// validateRequest enforces scheme restrictions.  It is called for the initial
// request and each redirect hop via CheckRedirect.  IP/host validation is
// performed at dial time by safeDial so the checked address is exactly the
// address we connect to, eliminating the DNS-rebinding TOCTOU.
func (h *Http) validateRequest(req *http.Request) error {
	switch req.URL.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("scheme %q is not allowed; only http and https are permitted", req.URL.Scheme)
	}
	return nil
}

// safeDial resolves address to candidate IPs, rejects internal IPs (when
// AllowInternalTargets=false), and dials the resolved IP literal directly.
// Performing both the IP check and the connect against the same resolved
// address eliminates the DNS-rebinding TOCTOU that exists when validation
// and connect each perform their own independent resolution.
func (h *Http) safeDial(ctx context.Context, dialer *net.Dialer, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses found for %s", host)
	}

	if !h.opts.AllowInternalTargets {
		// Reject if ANY candidate is internal — prevents an attacker from
		// returning [public, private] and hoping fallback hits the private IP.
		for _, ipAddr := range ips {
			if isInternalIP(ipAddr.IP) {
				return nil, fmt.Errorf("dial to %s rejected: resolved to internal address %s (use --allow-internal-targets to override)", host, ipAddr.IP)
			}
		}
	}

	// Dial each candidate by IP literal until one succeeds. Bracket IPv6.
	var lastErr error
	for _, ipAddr := range ips {
		ipStr := ipAddr.IP.String()
		if ipAddr.IP.To4() == nil {
			ipStr = "[" + ipStr + "]"
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ipStr, port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
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
