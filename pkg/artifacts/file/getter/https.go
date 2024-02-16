package getter

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/rancherfederal/hauler/pkg/artifacts"
	"github.com/rancherfederal/hauler/pkg/consts"
)

type Http struct{}

func NewHttp() *Http {
	return &Http{}
}

func (h Http) Name(u *url.URL) string {
	resp, err := http.Head(u.String())
	if err != nil {
		return ""
	}

	name, _ := url.PathUnescape(u.String())
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

	// TODO: Not this
	return filepath.Base(name)
}

func (h Http) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
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
		config{Reference: u.String()},
	}
	return artifacts.ToConfig(c, artifacts.WithConfigMediaType(consts.FileHttpConfigMediaType))
}

type httpConfig struct {
	config `json:",inline,omitempty"`
}
