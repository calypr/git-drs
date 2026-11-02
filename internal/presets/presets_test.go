package presets

import (
	"strings"
	"testing"
)

func TestBuiltInCatalog(t *testing.T) {
	items, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d presets, want 4", len(items))
	}
	for _, alias := range []string{"calypr", "terra", "synapse", "cgc"} {
		p, ok, err := Lookup(alias)
		if err != nil || !ok || p.Endpoint == "" || p.Provider == "" || p.Auth == "" {
			t.Fatalf("invalid %s preset: %+v, %v", alias, p, err)
		}
	}
}

func TestCatalogRequiresPresetAuthenticationType(t *testing.T) {
	_, err := loadCatalog([]byte(`
version: 1
presets:
  - alias: unauthenticated
    endpoint: https://drs.example.org
    provider: ga4gh
`))
	if err == nil {
		t.Fatal("expected a preset without an authentication type to be rejected")
	}
	if !strings.Contains(err.Error(), `preset "unauthenticated" does not specify an authentication type`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
