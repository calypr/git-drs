package anvil_client

import (
	"io"
	"log"
	"strings"
	"testing"
)

func TestAnvilRemoteAccessors(t *testing.T) {
	remote := AnvilRemote{
		Endpoint: "https://example.org/drs",
		Auth: AnvilAuth{
			TerraProject: "terra-project",
		},
	}

	if got := remote.GetProjectId(); got != "terra-project" {
		t.Fatalf("GetProjectId() = %q", got)
	}

	if got := remote.GetEndpoint(); got != "https://example.org/drs" {
		t.Fatalf("GetEndpoint() = %q", got)
	}

	if got := remote.GetBucketName(); got != "" {
		t.Fatalf("GetBucketName() = %q", got)
	}
}

func TestAnvilRemoteGetClientNotImplemented(t *testing.T) {
	remote := AnvilRemote{}
	client, err := remote.GetClient(map[string]string{}, log.New(io.Discard, "", 0))
	if err == nil {
		t.Fatalf("expected error")
	}
	if client != nil {
		t.Fatalf("expected nil client")
	}
	if !strings.Contains(err.Error(), "AnVIL Client needs to be implemented") {
		t.Fatalf("unexpected error: %v", err)
	}
}
