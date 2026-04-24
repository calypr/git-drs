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

func TestRequestMatchesEndpointHost(t *testing.T) {
	tests := []struct {
		name     string
		req      credentialRequest
		endpoint string
		want     bool
	}{
		{
			name:     "matches exact host",
			req:      credentialRequest{Host: "example.org"},
			endpoint: "https://example.org/api/v1",
			want:     true,
		},
		{
			name:     "matches host with port",
			req:      credentialRequest{Host: "127.0.0.1:8080"},
			endpoint: "http://127.0.0.1:8080/drs",
			want:     true,
		},
		{
			name:     "rejects different host",
			req:      credentialRequest{Host: "gogs.local"},
			endpoint: "http://127.0.0.1:8080/drs",
			want:     false,
		},
		{
			name:     "rejects empty host",
			req:      credentialRequest{},
			endpoint: "http://127.0.0.1:8080/drs",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requestMatchesEndpointHost(tt.req, tt.endpoint)
			if got != tt.want {
				t.Fatalf("requestMatchesEndpointHost(%+v, %q) = %v, want %v", tt.req, tt.endpoint, got, tt.want)
			}
		})
	}
}
