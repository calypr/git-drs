package add

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/data-client/credentials"
	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	conf "github.com/calypr/syfon/client/config"
	"github.com/spf13/cobra"
)

var Gen3Cmd = &cobra.Command{
	Use: "gen3 [remote-name]",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs remote add gen3 --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		// make sure at least one of the credentials params is provided
		if credFile == "" && fenceToken == "" && len(args) == 0 {
			return fmt.Errorf("error: Gen3 requires a credentials file or accessToken to setup project locally. Please provide either a --cred or --token flag. See 'git drs remote add gen3 --help' for more details")
		}

		remoteName := config.ORIGIN
		if len(args) > 0 {
			remoteName = args[0]
		}

		err := gen3Init(remoteName, credFile, fenceToken, project, organization, bucket, logg)
		if err != nil {
			return fmt.Errorf("error configuring gen3 server: %v", err)
		}
		return nil
	},
}

func gen3Init(remoteName, credFile, fenceToken, project, organization, bucket string, logg *slog.Logger) error {
	if remoteName == "" {
		return fmt.Errorf("remote name is required")
	}
	if project == "" {
		return fmt.Errorf("project is required for Gen3 remote")
	}

	resolvedBucket := strings.TrimSpace(bucket)
	resolvedStoragePrefix := ""
	if strings.TrimSpace(organization) != "" {
		scope, err := gitrepo.ResolveBucketScope(organization, project, resolvedBucket, "")
		if err != nil {
			return fmt.Errorf("failed resolving bucket mapping for organization=%q project=%q: %w", organization, project, err)
		}
		resolvedBucket = strings.TrimSpace(scope.Bucket)
		resolvedStoragePrefix = strings.TrimSpace(scope.Prefix)
	}
	if resolvedBucket == "" {
		if strings.TrimSpace(organization) == "" {
			return fmt.Errorf("bucket is required when organization is empty")
		}
		if strings.TrimSpace(resolvedBucket) == "" {
			return fmt.Errorf("bucket is required (or configure mapping first with `git drs bucket add-project --organization %s --project %s --path <scheme>://<bucket>/<prefix>`)", organization, project)
		}
	}

	var accessToken, apiKey, keyID, apiEndpoint string
	configure := conf.NewConfigure(logg)
	switch {
	case fenceToken != "":
		accessToken = fenceToken
		var err error
		apiEndpoint, err = common.ParseAPIEndpointFromToken(accessToken)
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

		apiEndpoint, err = common.ParseAPIEndpointFromToken(cred.APIKey)
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

	remoteGen3 := config.RemoteSelect{
		Gen3: &config.Gen3Remote{
			Endpoint:      apiEndpoint,
			ProjectID:     project,
			Organization:  organization,
			Bucket:        resolvedBucket,
			StoragePrefix: resolvedStoragePrefix,
		},
	}

	remote := config.Remote(remoteName)
	if _, err := config.UpdateRemote(remote, remoteGen3); err != nil {
		return fmt.Errorf("failed to update remote config: %w", err)
	}
	logg.Debug(fmt.Sprintf("Remote added/updated: %s → %s (project: %s, bucket: %s, storage_prefix: %s)", remoteName, apiEndpoint, project, resolvedBucket, resolvedStoragePrefix))

	// Step 3: Ensure credential profile is up-to-date (refreshes token if needed)
	cred := &conf.Credential{
		Profile:            remoteName,
		APIEndpoint:        apiEndpoint,
		APIKey:             apiKey,
		KeyID:              keyID,
		AccessToken:        accessToken, // may be stale
		UseShepherd:        "false",     // or preserve from existing?
		MinShepherdVersion: "",
	}

	if err := credentials.EnsureValidCredential(context.Background(), cred, logg); err != nil {
		return fmt.Errorf("failed to verify/refresh Gen3 credential: %w", err)
	}

	if err := configure.Save(cred); err != nil {
		return fmt.Errorf("failed to configure/update Gen3 profile: %w", err)
	}
	// Configure stock git credential plumbing for lfs + persist the refreshed token locally.
	if err := gitrepo.ConfigureCredentialHelperForRepo(); err != nil {
		return fmt.Errorf("failed to configure git credential helper: %w", err)
	}
	if err := gitrepo.SetRemoteLFSURL(remoteName, apiEndpoint); err != nil {
		return fmt.Errorf("failed to set lfs url for remote %s: %w", remoteName, err)
	}
	if strings.TrimSpace(cred.AccessToken) != "" {
		if err := gitrepo.SetRemoteToken(remoteName, strings.TrimSpace(cred.AccessToken)); err != nil {
			return fmt.Errorf("failed to persist repo token for remote %s: %w", remoteName, err)
		}
	}

	logg.Debug(fmt.Sprintf("Gen3 profile '%s' configured and token refreshed successfully", remoteName))
	return nil
}
