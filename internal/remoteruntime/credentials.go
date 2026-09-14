package remoteruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	syconf "github.com/calypr/syfon/client/config"
	syrequest "github.com/calypr/syfon/client/request"
)

// EnsureValidCredential validates a Gen3 credential and refreshes an expired
// access token with its API key when possible.
func EnsureValidCredential(ctx context.Context, cred *syconf.Credential, logger *slog.Logger) error {
	if cred == nil {
		return fmt.Errorf("invalid credential: credential is nil")
	}
	if cred.AccessToken != "" && cred.AccessToken == cred.APIKey {
		return fmt.Errorf("invalid credential: access token and API key must differ")
	}

	manager := syconf.NewConfigure(logger)
	accessTokenValid, accessErr := manager.IsTokenValid(cred.AccessToken)
	if accessTokenValid {
		return nil
	}
	apiKeyValid, apiKeyErr := manager.IsTokenValid(cred.APIKey)
	if !apiKeyValid {
		return fmt.Errorf("invalid credential: both access token and API key are invalid: %v; %v", accessErr, apiKeyErr)
	}

	transport := &syrequest.AuthTransport{
		Base: http.DefaultTransport,
		Cred: cred,
		Mode: syrequest.AuthModeBearer,
	}
	if refreshErr := transport.NewAccessToken(ctx); refreshErr != nil {
		return fmt.Errorf("failed to refresh access token: %v (original error: %v)", refreshErr, accessErr)
	}
	saveRefreshedCredential(manager, cred, logger)
	return nil
}

func saveRefreshedCredential(manager *syconf.Manager, cred *syconf.Credential, logger *slog.Logger) {
	if err := manager.Save(cred); err != nil && logger != nil {
		logger.Warn("failed to save refreshed token", "error", err)
	}
}
