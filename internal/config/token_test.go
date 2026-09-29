package config

import (
	"strings"
	"testing"
)

func TestParseAPIEndpointFromToken(t *testing.T) {
	tokenString := "eyJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJodHRwczovL2NvbW1vbnMuZXhhbXBsZS5vcmcvIn0.c2ln"

	endpoint, err := ParseAPIEndpointFromToken(tokenString)
	if err != nil {
		t.Fatalf("ParseAPIEndpointFromToken error: %v", err)
	}
	if endpoint != "https://commons.example.org" {
		t.Fatalf("endpoint = %q, want https://commons.example.org", endpoint)
	}
}

func TestParseAPIEndpointFromTokenErrors(t *testing.T) {
	secretMarker := "token-secret-marker"
	tests := []string{
		"invalid-token",
		"eyJhbGciOiJIUzI1NiJ9.e30.signature",
		secretMarker,
	}
	for _, tokenString := range tests {
		if _, err := ParseAPIEndpointFromToken(tokenString); err == nil {
			t.Fatalf("expected error for token %q", tokenString)
		} else if strings.Contains(err.Error(), secretMarker) {
			t.Fatalf("token was included in parse error: %v", err)
		}
	}
}
