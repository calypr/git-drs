package remoteruntime

import (
	"context"
	"strings"
	"testing"

	syconf "github.com/calypr/syfon/client/config"
)

func TestEnsureValidCredentialRejectsReusedAPIKey(t *testing.T) {
	t.Parallel()

	err := EnsureValidCredential(context.Background(), &syconf.Credential{
		AccessToken: "same-secret",
		APIKey:      "same-secret",
	}, nil)
	if err == nil {
		t.Fatal("expected reused API key to be rejected")
	}
	if !strings.Contains(err.Error(), "access token and API key must differ") {
		t.Fatalf("unexpected error: %v", err)
	}
}
