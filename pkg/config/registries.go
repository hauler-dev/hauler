package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

type Registries struct {
	Mirrors map[string]Mirror `yaml:"mirrors"`
}

type Mirror struct {
	Endpoints []string `yaml:"endpoint"`
}

func RegistriesConfigFromFile(path string) (*Registries, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read registries config: %w", err)
	}

	var registries Registries
	if err := yaml.Unmarshal(data, &registries); err != nil {
		return nil, fmt.Errorf("failed to parse registries config: %w", err)
	}

	return &registries, nil
}
