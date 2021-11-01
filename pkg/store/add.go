package store

import (
	"context"

	"github.com/rancherfederal/hauler/pkg/content"
	"github.com/rancherfederal/hauler/pkg/log"
)

type addOptions struct {
	repo string
}

type AddOption func(*addOptions)

func makeAddOptions(opts ...AddOption) addOptions {
	opt := addOptions{}
	for _, o := range opts {
		o(&opt)
	}
	return opt
}

func (s *Store) Add(ctx context.Context, oci content.Oci, opts ...AddOption) error {
	l := log.FromContext(ctx)
	opt := makeAddOptions(opts...)

	if err := s.precheck(); err != nil {
		return err
	}

	if opt.repo == "" {
	}

	if err := oci.Copy(ctx, s.RegistryURL()); err != nil {
		return err
	}

	_ = l
	return nil
}

func OverrideRepo(r string) AddOption {
	return func(opts *addOptions) {
		opts.repo = r
	}
}
