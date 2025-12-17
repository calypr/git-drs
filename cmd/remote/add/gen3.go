package add

import (
	"fmt"

	"github.com/calypr/data-client/client/jwt"
	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var Gen3Cmd = &cobra.Command{
	Use:  "gen3",
	Args: cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		// make sure at least one of the credentials params is provided
		if credFile == "" && fenceToken == "" && len(args) == 0 {
			return fmt.Errorf("Error: Gen3 requires a credentials file or accessToken to setup project locally. Please provide either a --cred or --token flag. See 'git drs init --help' for more details")
		}

		// When adding a new remote, bucket field is required.
		if bucket == "" {
			return fmt.Errorf("Error: Gen3 requires a bucket name to be specified when adding a new remote specify a bucket with --bucket flag. See 'git drs init --help' for more details")
		}

		remoteName := config.ORIGIN
		if len(args) > 0 {
			remoteName = args[0]
		}

		err := gen3Init(remoteName, credFile, fenceToken, project, bucket, logg)
		if err != nil {
			return fmt.Errorf("Error configuring gen3 server: %v", err)
		}
		return nil
	},
}

func gen3Init(remoteName, credFile, fenceToken, project, bucket string, log *drslog.Logger) error {
	if remoteName == "" {
		return fmt.Errorf("remote name is required")
	}
	if project == "" || bucket == "" {
		return fmt.Errorf("project and bucket are required for Gen3 remote")
	}

	var accessToken, apiKey, keyID, apiEndpoint string
	var conf jwt.Configure
	switch {
	case fenceToken != "":
		accessToken = fenceToken
		var err error
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(accessToken)
		if err != nil {
			return fmt.Errorf("failed to parse API endpoint from provided access token: %w", err)
		}

	case credFile != "":
		cred, err := conf.ReadCredentials(credFile, "")
		if err != nil {
			return fmt.Errorf("failed to read credentials file %s: %w", credFile, err)
		}
		accessToken = cred.AccessToken
		apiKey = cred.APIKey
		keyID = cred.KeyId

		apiEndpoint, err = utils.ParseAPIEndpointFromToken(cred.APIKey)
		if err != nil {
			return fmt.Errorf("failed to parse API endpoint from API key in credentials file: %w", err)
		}

	default:
		existing, err := conf.ParseConfig(remoteName)
		if err == nil {
			accessToken = existing.AccessToken
			apiKey = existing.APIKey
			keyID = existing.KeyId
			apiEndpoint = existing.APIEndpoint
		} else {
			return fmt.Errorf("must provide either --cred or --token (or have existing profile %s)", remoteName)
		}
	}

	if apiEndpoint == "" {
		return fmt.Errorf("could not determine Gen3 API endpoint")
	}

	remoteGen3 := config.RemoteSelect{
		Gen3: &indexd_client.Gen3Remote{
			Endpoint:  apiEndpoint,
			ProjectID: project,
			Bucket:    bucket,
		},
	}

	remote := config.Remote(remoteName)
	if _, err := config.UpdateRemote(remote, remoteGen3); err != nil {
		return fmt.Errorf("failed to update remote config: %w", err)
	}
	log.Printf("Remote added/updated: %s → %s (project: %s, bucket: %s)", remoteName, apiEndpoint, project, bucket)

	// Step 3: Ensure credential profile is up-to-date (refreshes token if needed)
	cred := &jwt.Credential{
		Profile:            remoteName,
		APIEndpoint:        apiEndpoint,
		APIKey:             apiKey,
		KeyId:              keyID,
		AccessToken:        accessToken, // may be stale
		UseShepherd:        "false",     // or preserve from existing?
		MinShepherdVersion: "",
	}

	if err := jwt.UpdateConfig(&indexd_client.Gen3LoggerAdapter{Logger: drslog.GetLogger()}, cred); err != nil {
		return fmt.Errorf("failed to configure/update Gen3 profile: %w", err)
	}

	log.Printf("Gen3 profile '%s' configured and token refreshed successfully", remoteName)
	return nil
}
