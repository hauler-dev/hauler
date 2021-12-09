package chart

import (
	"fmt"

	gname "github.com/google/go-containerregistry/pkg/name"

	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
	"github.com/rancherfederal/hauler/pkg/artifact"
	"github.com/rancherfederal/hauler/pkg/content/chart"
	"github.com/rancherfederal/hauler/pkg/content/image"
)

var _ artifact.Collection = (*tchart)(nil)

// tchart is a thick chart that includes all the dependent images as well as the chart itself
type tchart struct {
	chart  *chart.Chart
	config v1alpha1.ThickChart

	computed bool
	contents map[string]artifact.OCI
}

func NewThickChart(cfg v1alpha1.ThickChart) (artifact.Collection, error) {
	o, err := chart.NewChart(cfg.Name, cfg.RepoURL, cfg.Version)
	if err != nil {
		return nil, fmt.Errorf("create chart: %w", err)
	}

	return &tchart{
		chart:    o,
		config:   cfg,
		contents: make(map[string]artifact.OCI),
	}, nil
}

func (c *tchart) Contents() (map[string]artifact.OCI, error) {
	if err := c.compute(); err != nil {
		return nil, fmt.Errorf("compute content: %w", err)
	}
	return c.contents, nil
}

func (c *tchart) compute() error {
	if c.computed {
		return nil
	}

	if err := c.dependentImages(); err != nil {
		return fmt.Errorf("dependent images: %w", err)
	}
	if err := c.chartContents(); err != nil {
		return fmt.Errorf("chart contents: %w", err)
	}
	if err := c.extraImages(); err != nil {
		return fmt.Errorf("extra images: %w", err)
	}

	c.computed = true
	return nil
}

func (c *tchart) chartContents() error {
	oci, err := chart.NewChart(c.config.Name, c.config.RepoURL, c.config.Version)
	if err != nil {
		return fmt.Errorf("create chart: %w", err)
	}

	tag := c.config.Version
	if tag == "" {
		tag = gname.DefaultTag
	}

	c.contents[c.config.Name] = oci
	return nil
}

func (c *tchart) dependentImages() error {
	ch, err := c.chart.Load()
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}

	imgs, err := ImagesInChart(ch)
	if err != nil {
		return fmt.Errorf("images in chart: %w", err)
	}

	for _, img := range imgs.Spec.Images {
		i, err := image.NewImage(img.Ref)
		if err != nil {
			return fmt.Errorf("create image %s: %w", img.Ref, err)
		}
		c.contents[img.Ref] = i
	}
	return nil
}

func (c *tchart) extraImages() error {
	for _, img := range c.config.ExtraImages {
		i, err := image.NewImage(img.Reference)
		if err != nil {
			return fmt.Errorf("create image %s: %w", img.Reference, err)
		}
		c.contents[img.Reference] = i
	}
	return nil
}
