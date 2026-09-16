package auth

import (
	"strings"
	"testing"
)

func TestGlobusCommandOffersLoginLifecycle(t *testing.T) {
	for _, action := range []string{"login", "status", "logout"} {
		if !strings.Contains(GlobusCmd.Use, action) {
			t.Fatalf("Globus command usage does not include %q", action)
		}
	}
	if GlobusCmd.Flag("scope") == nil {
		t.Fatal("Globus login must accept dependent OAuth scopes")
	}
}
