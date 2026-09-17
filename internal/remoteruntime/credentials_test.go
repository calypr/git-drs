package remoteruntime

import (
	"context"
	"errors"
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

func TestRedactCredentialErrorRemovesSecrets(t *testing.T) {
	cred := &syconf.Credential{AccessToken: "access-secret", APIKey: "api-secret"}
	err := redactCredentialError(errors.New("request failed with access-secret and api-secret"), cred)
	if strings.Contains(err.Error(), cred.AccessToken) || strings.Contains(err.Error(), cred.APIKey) {
		t.Fatalf("credential error leaked secret: %v", err)
	}
}
