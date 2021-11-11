package store

import "github.com/rancherfederal/hauler/pkg/cache"

// Options defines options for Store
type Options func(*Store)

// WithCache initializes a Store with a cache.Cache, all content added to the Store will first be cached
func WithCache(c cache.Cache) Options {
	return func(s *Store) {
		s.cache = c
	}
}

// WithDefaultRepository sets the default repository to use when none is specified (defaults to "library")
func WithDefaultRepository(repo string) Options {
	return func(s *Store) {
		s.DefaultRepository = repo
	}
}
