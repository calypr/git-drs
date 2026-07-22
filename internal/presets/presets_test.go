package presets

import "testing"

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
