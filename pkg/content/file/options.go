package file

import "github.com/rancherfederal/hauler/internal/getter"

type Option func(*file)

func WithClient(c *getter.Client) Option {
	return func(f *file) {
		f.client = c
	}
}
