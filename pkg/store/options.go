package store

import (
	"github.com/rancherfederal/hauler/internal/cache"
)

type Options func(*Store)

func WithCache(c cache.Cache) Options {
	return func(b *Store) {
		b.cache = c
	}
}
