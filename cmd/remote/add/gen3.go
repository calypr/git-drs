package add

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/calypr/calypr-cli/conf"
	"github.com/calypr/calypr-cli/credentials"
	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/spf13/cobra"
)

var Gen3Cmd = &cobra.Command{
	Use: "gen3 [remote-name] <organization/project>",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 || len(args) > 2 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: expected [remote-name] <organization/project>, received %d arguments\n\nUsage: %s\n\nSee 'git drs remote add gen3 --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		remoteName := config.ORIGIN
		scopeArg := ""
		if len(args) == 1 {
			scopeArg = args[0]
		} else {
			remoteName = args[0]
			scopeArg = args[1]
		}

		err := gen3Init(remoteName, credFile, fenceToken, scopeArg, logg)
		if err != nil {
			return fmt.Errorf("error configuring gen3 server: %v", err)
		}
		if noSkipSmudge {
			if err := gitrepo.SetGitConfigOptions(map[string]string{"drs.skipsmudge": "false"}); err != nil {
				return fmt.Errorf("failed to configure skipsmudge: %w", err)
			}
		}
		return nil
	},
}

func gen3Init(remoteName, credFile, fenceToken, scopeArg string, logg *slog.Logger) error {
	if remoteName == "" {
		return fmt.Errorf("remote name is required")
	}
	if err := initialize.EnsureInitialized(logg); err != nil {
		return fmt.Errorf("failed to initialize repository: %w", err)
	}
	organization, project, err := parseScopeArg(scopeArg)
	if err != nil {
		return err
	}

	var accessToken, apiKey, keyID, apiEndpoint string
	configure := conf.NewConfigure(logg)
	switch {
	case fenceToken != "":
		accessToken = fenceToken
		var err error
		apiEndpoint, err = config.ParseAPIEndpointFromToken(accessToken)
		if err != nil {
			return fmt.Errorf("failed to parse API endpoint from provided access token: %w", err)
		}

	case credFile != "":
		cred, err := configure.Import(credFile, "")
		if err != nil {
			return fmt.Errorf("failed to read credentials file %s: %w", credFile, err)
		}
		accessToken = cred.AccessToken
		apiKey = cred.APIKey
		keyID = cred.KeyID

		apiEndpoint, err = config.ParseAPIEndpointFromToken(cred.APIKey)
		if err != nil {
			return fmt.Errorf("failed to parse API endpoint from API key in credentials file: %w", err)
		}

	default:
		existing, err := configure.Load(remoteName)
		if err != nil {
			return fmt.Errorf("failed to load %s config: %w", remoteName, err)
		} else {
			accessToken = existing.AccessToken
			apiKey = existing.APIKey
			keyID = existing.KeyID
			apiEndpoint = existing.APIEndpoint
		}
	}

	if apiEndpoint == "" {
		return fmt.Errorf("could not determine Gen3 API endpoint")
	}

	cred := &conf.Credential{
		Profile:            remoteName,
		APIEndpoint:        apiEndpoint,
		APIKey:             apiKey,
		KeyID:              keyID,
		AccessToken:        accessToken, // may be stale
		UseShepherd:        "false",
		MinShepherdVersion: "",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := credentials.EnsureValidCredential(ctx, cred, logg); err != nil {
		return fmt.Errorf("failed to verify/refresh Gen3 credential: %w", remoteruntime.WrapCredentialValidationError(remoteName, err))
	}

	scope, err := gitrepo.ResolveBucketScope(organization, project, "", "")
	if err != nil {
		scope, err = resolveBucketScopeFromServer(context.Background(), apiEndpoint, strings.TrimSpace(cred.AccessToken), organization, project, selectedBucket)
		if err != nil {
			return fmt.Errorf("failed resolving bucket mapping for organization=%q project=%q: %w", organization, project, err)
		}
	}
	resolvedBucket := strings.TrimSpace(scope.Bucket)
	resolvedStoragePrefix := strings.TrimSpace(scope.Prefix)
	if resolvedBucket == "" {
		return fmt.Errorf("no bucket mapping found for organization=%q project=%q", organization, project)
	}

	if err := persistGen3Remote(remoteName, organization, project, apiEndpoint, scope, func() error {
		return configure.Save(cred)
	}); err != nil {
		return err
	}
	logg.Debug(fmt.Sprintf("Remote added/updated: %s → %s (project: %s, bucket: %s, storage_prefix: %s)", remoteName, apiEndpoint, project, resolvedBucket, resolvedStoragePrefix))

	logg.Debug(fmt.Sprintf("Gen3 profile '%s' configured and token refreshed successfully", remoteName))
	return nil
}
