package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	dconf "github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/fence"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/data-client/request"
	syconf "github.com/calypr/syfon/client/conf"
)

// EnsureValidCredential validates a profile credential and refreshes access token
// from API key when possible.
func EnsureValidCredential(ctx context.Context, cred *syconf.Credential, baseLogger *slog.Logger) error {
	manager := dconf.NewConfigure(baseLogger)
	logger := logs.NewGen3Logger(baseLogger, "", cred.Profile)

	valid, err := manager.IsCredentialValid(cred)
	if valid {
		return nil
	}
	if err == nil {
		return fmt.Errorf("invalid credential")
	}

	// Keep legacy behavior: only auto-refresh when API key is valid but token expired.
	if !strings.Contains(err.Error(), "access_token is invalid but api_key is valid") {
		return fmt.Errorf("invalid credential: %v", err)
	}

	req := request.NewRequestInterface(logger, cred, manager)
	fClient := fence.NewFenceClient(req, cred, baseLogger)
	newToken, refreshErr := fClient.NewAccessToken(ctx)
	if refreshErr != nil {
		return fmt.Errorf("failed to refresh access token: %v (original error: %v)", refreshErr, err)
	}

	cred.AccessToken = newToken
	if saveErr := manager.Save(cred); saveErr != nil {
		logger.Warn(fmt.Sprintf("failed to save refreshed token: %v", saveErr))
	}
	return nil
}
