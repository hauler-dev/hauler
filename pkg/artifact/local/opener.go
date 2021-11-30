package local

import (
	"io"
	"net/http"
	"os"
)

type Opener func() (io.ReadCloser, error)

func LocalOpener(path string) Opener {
	return func() (io.ReadCloser, error) {
		return os.Open(path)
	}
}

func RemoteOpener(url string) Opener {
	return func() (io.ReadCloser, error) {
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
}
