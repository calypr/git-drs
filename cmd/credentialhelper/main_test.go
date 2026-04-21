package credentialhelper

import (
	"strings"
	"testing"
)

func TestReadCredentialRequest(t *testing.T) {
	in := strings.NewReader("protocol=https\nhost=example.org\npath=repo.git/info/lfs\n\n")
	req, err := readCredentialRequest(in)
	if err != nil {
		t.Fatalf("readCredentialRequest returned error: %v", err)
	}
	if req.Protocol != "https" {
		t.Fatalf("expected protocol https, got %q", req.Protocol)
	}
	if req.Host != "example.org" {
		t.Fatalf("expected host example.org, got %q", req.Host)
	}
	if req.Path != "repo.git/info/lfs" {
		t.Fatalf("expected path repo.git/info/lfs, got %q", req.Path)
	}
}
