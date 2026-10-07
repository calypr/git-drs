// Package presets exposes the non-secret remote defaults shipped with git-drs.
package presets

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// CatalogVersion is persisted when a preset is expanded.
const CatalogVersion = 1

type Preset struct {
	Alias             string `yaml:"alias"`
	Endpoint          string `yaml:"endpoint"`
	Provider          string `yaml:"provider"`
	Auth              string `yaml:"auth"`
	RegistryServiceID string `yaml:"registry_service_id,omitempty"`
}

type catalog struct {
	Version int      `yaml:"version"`
	Presets []Preset `yaml:"presets"`
}

//go:embed presets.yaml
var builtIn []byte

func List() ([]Preset, error) {
	return loadCatalog(builtIn)
}

func loadCatalog(data []byte) ([]Preset, error) {
	var c catalog
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("decode built-in preset catalog: %w", err)
	}
	if c.Version != CatalogVersion {
		return nil, fmt.Errorf("unsupported built-in preset catalog version %d", c.Version)
	}
	for i, preset := range c.Presets {
		if strings.TrimSpace(preset.Auth) == "" {
			name := strings.TrimSpace(preset.Alias)
			if name == "" {
				name = fmt.Sprintf("at index %d", i)
			}
			return nil, fmt.Errorf("preset %q does not specify an authentication type", name)
		}
	}
	return append([]Preset(nil), c.Presets...), nil
}

func Lookup(alias string) (Preset, bool, error) {
	alias = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(alias)), "built-in:")
	items, err := List()
	if err != nil {
		return Preset{}, false, err
	}
	for _, item := range items {
		if item.Alias == alias {
			return item, true, nil
		}
	}
	return Preset{}, false, nil
}
