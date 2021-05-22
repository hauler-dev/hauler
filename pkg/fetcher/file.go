package fetcher

import (
	"context"
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type FileFetcher struct {
	fetcher
}

func (f FileFetcher) Get(ctx context.Context, src string, dst string) error {
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating file: %w", err)
	}
	defer out.Close()

	logrus.Infof("Getting %s from %s", dst, src)
	resp, err := http.Get(src)
	if err != nil {
		return fmt.Errorf("getting file: %w", err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func GetFileNameFromURL(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		fmt.Errorf("nop %v", err)
	}

	path := u.Path
	segments := strings.Split(path, "/")
	return segments[len(segments)-1]
}