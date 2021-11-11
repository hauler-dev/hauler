package chart

import (
	gname "github.com/google/go-containerregistry/pkg/name"

	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/image"
)

var _ artifact.Collection = (*tchart)(nil)

// tchart is a thick chart that includes all the dependent images as well as the chart itself
type tchart struct {
	chart *chart.Chart

	computed bool
	contents map[gname.Reference]artifact.OCI
}

func NewChart(name, repo, version string) (artifact.Collection, error) {
	o, err := chart.NewChart(name, repo, version)
	if err != nil {
		return nil, err
	}

	return &tchart{
		chart:    o,
		contents: make(map[gname.Reference]artifact.OCI),
	}, nil
}

func (c *tchart) Contents() (map[gname.Reference]artifact.OCI, error) {
	if err := c.compute(); err != nil {
		return nil, err
	}
	return c.contents, nil
}

func (c *tchart) compute() error {
	if c.computed {
		return nil
	}

	if err := c.dependentImages(); err != nil {
		return err
	}

	c.computed = true
	return nil
}

func (c *tchart) dependentImages() error {
	ch, err := c.chart.Load()
	if err != nil {
		return err
	}

	imgs, err := ImagesInChart(ch)
	if err != nil {
		return err
	}

	for _, img := range imgs.Spec.Images {
		ref, err := gname.ParseReference(img.Ref)
		if err != nil {
			return err
		}

		i, err := image.NewImage(img.Ref)
		if err != nil {
			return err
		}
		c.contents[ref] = i
	}

	return nil
}
