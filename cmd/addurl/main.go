package addurl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

// AddURLCmd represents the add-url command
var AddURLCmd = &cobra.Command{
	Use:   "add-url <url> <sha256>",
	Short: "Add a file to the Git DRS repo using an S3 URL",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 2 arguments (S3 URL and SHA256), received %d\n\nUsage: %s\n\nSee 'git drs add-url --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()

		// set git config lfs.allowincompletepush = true
		// set git config lfs.allowincompletepush = true
		if err := gitrepo.SetGitConfigOptions(map[string]string{"lfs.allowincompletepush": "true"}); err != nil {
			return fmt.Errorf("unable to configure git to push pointers: %v", err)
		}

		// Parse arguments
		s3URL := args[0]
		sha256 := args[1]
		awsAccessKey, _ := cmd.Flags().GetString(s3_utils.AWS_KEY_FLAG_NAME)
		awsSecretKey, _ := cmd.Flags().GetString(s3_utils.AWS_SECRET_FLAG_NAME)
		awsRegion, _ := cmd.Flags().GetString(s3_utils.AWS_REGION_FLAG_NAME)
		awsEndpoint, _ := cmd.Flags().GetString(s3_utils.AWS_ENDPOINT_URL_FLAG_NAME)
		remote, _ := cmd.Flags().GetString("remote")

		// if providing credentials, access key and secret must both be provided
		if (awsAccessKey == "" && awsSecretKey != "") || (awsAccessKey != "" && awsSecretKey == "") {
			return errors.New("incomplete credentials provided as environment variables. Please run `export " + s3_utils.AWS_KEY_ENV_VAR + "=<key>` and `export " + s3_utils.AWS_SECRET_ENV_VAR + "=<secret>` to configure both")
		}

		// if none provided, use default AWS configuration on file
		if awsAccessKey == "" && awsSecretKey == "" {
			myLogger.Debug("No AWS credentials provided. Using default AWS configuration from file.")
		}

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			return fmt.Errorf("error getting default remote: %v", err)
		}

		drsClient, err := cfg.GetRemoteClient(remoteName, myLogger)
		if err != nil {
			return fmt.Errorf("error getting current remote client: %v", err)
		}

		// Call client.AddURL to handle Gen3 interactions
		meta, err := drsClient.AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, awsRegion, awsEndpoint)
		if err != nil {
			return err
		}

		// Generate and add pointer file
		_, relFilePath, err := utils.ParseS3URL(s3URL)
		if err != nil {
			return fmt.Errorf("failed to parse S3 URL: %w", err)
		}
		if err := generatePointerFile(relFilePath, sha256, meta.Size); err != nil {
			return fmt.Errorf("failed to generate pointer file: %w", err)
		}
		myLogger.Debug("S3 URL successfully added to Git DRS repo.")
		return nil
	},
}

func init() {
	AddURLCmd.Flags().String(s3_utils.AWS_KEY_FLAG_NAME, os.Getenv(s3_utils.AWS_KEY_ENV_VAR), "AWS access key")
	AddURLCmd.Flags().String(s3_utils.AWS_SECRET_FLAG_NAME, os.Getenv(s3_utils.AWS_SECRET_ENV_VAR), "AWS secret key")
	AddURLCmd.Flags().String(s3_utils.AWS_REGION_FLAG_NAME, os.Getenv(s3_utils.AWS_REGION_ENV_VAR), "AWS S3 region")
	AddURLCmd.Flags().String(s3_utils.AWS_ENDPOINT_URL_FLAG_NAME, os.Getenv(s3_utils.AWS_ENDPOINT_URL_ENV_VAR), "AWS S3 endpoint")
	AddURLCmd.Flags().String("remote", "", "target remote DRS server (default: default_remote)")
}

func generatePointerFile(filePath string, sha256 string, fileSize int64) error {
	// Define the pointer file content
	pointerContent := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", sha256, fileSize)

	// Ensure the directory exists
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for pointer file: %w", err)
	}

	// Write the pointer file
	if err := os.WriteFile(filePath, []byte(pointerContent), 0644); err != nil {
		return fmt.Errorf("failed to write pointer file: %w", err)
	}

	// Add the pointer file to Git
	if err := gitrepo.AddFile(filePath); err != nil {
		return fmt.Errorf("failed to add pointer file to Git: %w", err)
	}

	return nil
}
