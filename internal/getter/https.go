package getter

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/rancherfederal/hauler/pkg/artifact"
)

type https struct{}

func (h https) Name(u *url.URL) string {
	resp, err := http.Head(u.String())
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
	return filepath.Base(u.String())
}

func (h https) Open(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (h https) Detect(u *url.URL) bool {
	switch u.Scheme {
	case "http", "https":
		return true
	}
	return false
}

func (h https) Config(u *url.URL) artifact.Config {
	c := &config{
		Reference: u.String(),
	}
	return artifact.ToConfig(c)
}
