package chart

import (
	"helm.sh/helm/v3/pkg/action"

	v1 "hauler.dev/go/hauler/pkg/apis/hauler.cattle.io/v1"
	"hauler.dev/go/hauler/pkg/artifacts"
	"hauler.dev/go/hauler/pkg/artifacts/image"
	"hauler.dev/go/hauler/pkg/content/chart"
	"hauler.dev/go/hauler/pkg/reference"
)

var _ artifacts.OCICollection = (*tchart)(nil)

// tchart is a thick chart that includes all the dependent images as well as the chart itself
type tchart struct {
	chart  *chart.Chart
	config v1.ThickChart

	computed bool
	contents map[string]artifacts.OCI
}

func NewThickChart(cfg v1.ThickChart, opts *action.ChartPathOptions) (artifacts.OCICollection, error) {
	o, err := chart.NewChart(cfg.Chart.Name, opts)
	if err != nil {
		return nil, err
	}

	return &tchart{
		chart:    o,
		config:   cfg,
		contents: make(map[string]artifacts.OCI),
	}, nil
}

func (c *tchart) Contents() (map[string]artifacts.OCI, error) {
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
	if err := c.chartContents(); err != nil {
		return err
	}
	if err := c.extraImages(); err != nil {
		return err
	}

	c.computed = true
	return nil
}

func (c *tchart) chartContents() error {
	ch, err := c.chart.Load()
	if err != nil {
		return err
	}

	name := ch.Name()
	if c.config.OverrideName != "" {
		name = c.config.OverrideName
	}

	if c.config.OverrideNamespace != "" {
		name = c.config.OverrideNamespace + "/" + name
	}

	ref, err := reference.NewTagged(name, ch.Metadata.Version)
	if err != nil {
		return err
	}
	c.contents[ref.Name()] = c.chart
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
		i, err := image.NewImage(img.Name)
		if err != nil {
			return err
		}
		c.contents[img.Name] = i
	}
	return nil
}

func (c *tchart) extraImages() error {
	for _, img := range c.config.ExtraImages {
		i, err := image.NewImage(img.Reference)
		if err != nil {
			return err
		}
		c.contents[img.Reference] = i
	}
	return nil
}
