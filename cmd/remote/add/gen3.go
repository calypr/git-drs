package add

import (
	"fmt"
	"log"
	"strings"

	"github.com/calypr/data-client/client/jwt"
	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var Gen3Cmd = &cobra.Command{
	Use:  "gen3",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		// make sure at least one of the credentials params is provided
		if credFile == "" && fenceToken == "" && profile == "" {
			return fmt.Errorf("Error: Gen3 requires a credentials file or accessToken to setup project locally. Please provide either a --cred or --token flag. See 'git drs init --help' for more details")
		}

		remoteName := config.ORIGIN
		if len(args) > 0 {
			remoteName = args[0]
		}

		err := gen3Init(remoteName, profile, credFile, fenceToken, project, bucket, logg)
		if err != nil {
			return fmt.Errorf("Error configuring gen3 server: %v", err)
		}

		return nil
	},
}

func gen3Init(remoteName string, profile string, credFile string, fenceToken string, project string, bucket string, log *log.Logger) error {
	// double check that one of the credentials params is provided

	var err error
	var cfg jwt.Credential
	if fenceToken == "" {
		cred := jwt.Configure{}
		if credFile == "" {
			cfg, err = cred.ParseConfig(profile)
			if err != nil {
				return err
			}
			fenceToken = cfg.AccessToken
		} else {
			log.Printf("Reading credential file: %s", credFile)
			optCredential, err := cred.ReadCredentials(credFile, "")
			if err != nil {
				return err
			}
			cfg = *optCredential
			fenceToken = cfg.AccessToken
		}
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(cfg.APIKey)
		if err != nil {
			log.Printf("Error parsing APIEndpoint: %s", err)
			return err
		}
	}

	if apiEndpoint == "" {
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(fenceToken)
		if err != nil {
			return err
		}
	}

	if credFile == "" && fenceToken == "" {
		return fmt.Errorf("Error: Gen3 requires a credentials file or accessToken to setup project locally")
	}

	if fenceToken == "" {
		cred := jwt.Configure{}
		credential, err := cred.ReadCredentials(credFile, "")
		if err != nil {
			return err
		}
		fenceToken = credential.AccessToken
	}
	if apiEndpoint == "" {
		apiEndpoint, err = utils.ParseAPIEndpointFromToken(fenceToken)
		if err != nil {
			return err
		}
	}

	// update config file with gen3 server info
	remoteGen3 := config.RemoteSelect{
		Gen3: &indexd_client.Gen3Remote{
			Endpoint: apiEndpoint,
			Auth: indexd_client.Gen3Auth{
				Profile:   profile,
				ProjectID: project,
				Bucket:    bucket,
			},
		},
	}

	log.Printf("Remote Added: %s", remoteName)
	_, err = config.UpdateRemote(remoteName, remoteGen3)
	if err != nil {
		return fmt.Errorf("Error: unable to update config file with the requested parameters: %v\n", err)
	}

	// authenticate with gen3
	// if no credFile is specified, don't go for the update
	if credFile != "" {
		cred := &jwt.Credential{
			Profile:            profile,
			APIEndpoint:        apiEndpoint,
			AccessToken:        fenceToken,
			UseShepherd:        "false",
			MinShepherdVersion: "",
			// TODO: Don't store profile specific credentials in a global variable
			//KeyId:              client.ProfileConfig.KeyId,
			//APIKey:             client.ProfileConfig.APIKey,
		}
		err = jwt.UpdateConfig(cred)
		if err != nil {
			errStr := fmt.Sprintf("[ERROR] unable to configure your gen3 profile: %v", err)
			if strings.Contains(errStr, "apiendpoint") {
				errStr += " If you are accessing an internal website, make sure you are connected to the internal network."
			}
			return fmt.Errorf("%s", errStr)
		}
	}

	return nil

}
