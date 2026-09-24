package getter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"hauler.dev/go/hauler/v2/pkg/artifacts"
	"hauler.dev/go/hauler/v2/pkg/consts"
	"hauler.dev/go/hauler/v2/pkg/content"
)

type Http struct {
	client *http.Client
}

func NewHttp(insecureSkipTLSVerify bool, caFile string) *Http {
	tr, err := content.BuildTransport(insecureSkipTLSVerify, caFile)
	if err != nil {
		return &Http{client: http.DefaultClient}
	}
	return &Http{client: &http.Client{Transport: tr}}
}

func (h Http) Name(u *url.URL) string {
	// Name the file without the URL's query or fragment, so a presigned URL's signature never ends up in it.
	c := *u
	c.RawQuery, c.Fragment = "", ""
	unescaped, err := url.PathUnescape(c.String())
	if err != nil {
		return ""
	}
	return filepath.Base(unescaped)
}

func (h Http) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, redactURLError(err, u)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, redactURLError(err, u)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status fetching %s: %s", redactURL(u), resp.Status)
	}
	return resp.Body, nil
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
		config{Reference: StripCredentials(u.String())},
	}
	return artifacts.ToConfig(c, artifacts.WithConfigMediaType(consts.FileHttpConfigMediaType))
}

// StripCredentials removes any user:password from a URL, and drops a presigned URL's whole query and fragment, keeping an unsigned URL's query so a re-sync from `store create manifest` can still fetch it.
func StripCredentials(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	presigned := isPresigned(u.RawQuery)
	if u.User == nil && !presigned {
		return raw
	}
	u.User = nil
	if presigned {
		u.RawQuery, u.Fragment = "", ""
	}
	return u.String()
}

// isPresigned reports whether a raw query carries presigned URL signing params (AWS, GCS, CloudFront, or Azure SAS).
func isPresigned(rawQuery string) bool {
	for _, p := range strings.Split(rawQuery, "&") {
		k, _, _ := strings.Cut(p, "=")
		if uk, err := url.QueryUnescape(k); err == nil {
			k = uk
		}
		k = strings.ToLower(k)
		if strings.HasPrefix(k, "x-amz-") || strings.HasPrefix(k, "x-goog-") || k == "signature" || k == "sig" {
			return true
		}
	}
	return false
}

// redactURLError swaps the URL net/http puts in a request error (which only masks the password) for the redacted one.
func redactURLError(err error, u *url.URL) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = redactURL(u)
	}
	return err
}

// redactURL drops credentials, query, and fragment so none of them end up in an error message.
func redactURL(u *url.URL) string {
	c := *u
	c.User, c.RawQuery, c.Fragment = nil, "", ""
	return c.String()
}

type httpConfig struct {
	config `json:",inline,omitempty"`
}
