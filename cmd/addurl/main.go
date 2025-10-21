package addurl

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

// AddURLCmd represents the add-url command
var AddURLCmd = &cobra.Command{
	Use:   "add-url <url> --sha256 <sha256>",
	Short: "Add a file to the Git DRS repo using an S3 URL",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("add-url requires 1 arg, but received %d\n\nUsage: %s\n\nFlags:\n%s", len(args), cmd.UseLine(), cmd.Flags().FlagUsages())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// set up logger
		myLogger, err := client.NewLogger("", false)
		if err != nil {
			fmt.Printf("Failed to open log file: %v", err)
			return err
		}
		defer myLogger.Close()

		// set git config lfs.allowincompletepush = true
		configCmd := exec.Command("git", "config", "lfs.allowincompletepush", "true")
		if err := configCmd.Run(); err != nil {
			return fmt.Errorf("Unable to configure git to push pointers: %v. Please change the .git/config file to include an [lfs] section with allowincompletepush = true", err)
		}

		// Parse arguments
		s3URL := args[0]
		sha256, _ := cmd.Flags().GetString("sha256")
		awsAccessKey, _ := cmd.Flags().GetString("aws-access-key")
		awsSecretKey, _ := cmd.Flags().GetString("aws-secret-key")
		regionFlag, _ := cmd.Flags().GetString("region")
		endpointFlag, _ := cmd.Flags().GetString("endpoint")

		// Determine AWS credentials source, same env var names as AWS SDK
		if awsAccessKey == "" || awsSecretKey == "" {
			awsAccessKey = os.Getenv("AWS_ACCESS_KEY_ID")
			awsSecretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
			if awsAccessKey == "" || awsSecretKey == "" {
				return errors.New("AWS credentials are required. Provide them via flags or environment variables. See git drs add-url --help for more info.")
			} else {
				fmt.Println("Using AWS credentials from environment variables.")
			}
		} else {
			fmt.Println("Using AWS credentials from command-line flags.")
		}

		// Call client.AddURL to handle Gen3 interactions
		fileSize, _, err := client.AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag)
		if err != nil {
			return err
		}

		// Generate and add pointer file
		_, relFilePath, err := utils.ParseS3URL(s3URL)
		if err != nil {
			return fmt.Errorf("failed to parse S3 URL: %w", err)
		}
		if err := generatePointerFile(relFilePath, sha256, fileSize); err != nil {
			return fmt.Errorf("failed to generate pointer file: %w", err)
		}
		fmt.Println("S3 URL successfully added to Git DRS repo.")
		return nil
	},
}

func init() {
	AddURLCmd.Flags().String("sha256", "", "[required] SHA256 hash of the file")
	AddURLCmd.Flags().String("aws-access-key", "", "AWS access key")
	AddURLCmd.Flags().String("aws-secret-key", "", "AWS secret key")
	AddURLCmd.Flags().String("region", "", "AWS S3 region")
	AddURLCmd.Flags().String("endpoint", "", "AWS S3 endpoint")
	AddURLCmd.MarkFlagRequired("sha256")
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
	cmd := exec.Command("git", "add", filePath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add pointer file to Git: %w", err)
	}

	return nil
}
