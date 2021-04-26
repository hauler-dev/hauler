package packager

import (
	"context"
	"fmt"
	"github.com/google/go-querystring/query"
	"github.com/hashicorp/go-getter"
	"github.com/sirupsen/logrus"
	"net/url"
	"path"
	"path/filepath"
)

type file struct {
	src string
}

type getOpts struct {
	Archive bool `url:"archive"`
	Filename string `url:"filename"`
	Checksum string `url:"checksum"`
}

func NewFile(src string) file {
	f := file{
		src: src,
	}
	return f
}

func (f *file) Get(ctx context.Context, dst string) (string, error) {
	src := f.buildUrl()

	ofile, err := f.fileNameFromUrl()
	if err != nil {
		return "", err
	}

	filename := filepath.Join(dst, ofile)

	logrus.Infof("Retriving `%s` to `%s`", src, filename)

	gtr := getter.Client{
		Src: src,
		Dst: filename,

		// TODO: Is this ever not a file?
		Mode: getter.ClientModeFile,
	}

	if err := gtr.Get(); err != nil {
		return "", fmt.Errorf("failed to fetch file: %v", err)
	}

	return filename, nil
}

func (f *file) buildUrl() string {
	g := getOpts{
		Archive:  false,
		Filename: "",
		Checksum: "",
	}

	v, _ := query.Values(g)

	return fmt.Sprintf("%s?%s", f.src, v.Encode())
}

func (f *file) fileNameFromUrl() (string, error) {
	u, err := url.Parse(f.src)
	if err != nil {
		return "", err
	}

	return path.Base(u.Path), err
}