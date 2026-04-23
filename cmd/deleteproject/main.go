package deleteproject

import (
	"context"
	"fmt"
	"os"

	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	syfonclient "github.com/calypr/syfon/client/syfonclient"
	"github.com/spf13/cobra"
)

var (
	remote      string
	confirmFlag string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:    "delete-project <project_id>",
	Short:  "Delete all DRS objects for a given project",
	Long:   "Delete all DRS objects for a given project",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectId := args[0]
		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			return fmt.Errorf("error getting default remote: %v", err)
		}

		drsClient, err := cfg.GetRemoteClient(remoteName, logger)
		if err != nil {
			logger.Error(fmt.Sprintf("error creating DRS client: %s", err))
			return err
		}

		remoteConfig := cfg.GetRemote(remoteName)
		organization := ""
		if remoteConfig != nil {
			organization = remoteConfig.GetOrganization()
		}

		// Get a sample record to show the user what will be deleted
		sampleResp, err := drsClient.Client.Index().List(context.Background(), syfonclient.ListRecordsOptions{
			Organization: organization,
			ProjectID:    projectId,
			Limit:        1,
			Page:         1,
		})
		if err != nil {
			return fmt.Errorf("error getting sample records for project %s: %v", projectId, err)
		}

		// Show details and get confirmation unless --confirm flag matches project_id
		if confirmFlag != "" && confirmFlag != projectId {
			return fmt.Errorf("error: --confirm value '%s' does not match project ID '%s'", confirmFlag, projectId)
		}
		if confirmFlag != projectId {
			common.DisplayWarningHeader(os.Stderr, "DELETE ALL RECORDS for a project")
			common.DisplayField(os.Stderr, "Remote", string(remoteName))
			common.DisplayField(os.Stderr, "Project ID", projectId)

			if sampleResp.Records != nil && len(*sampleResp.Records) > 0 {
				sample := (*sampleResp.Records)[0]
				fmt.Fprintf(os.Stderr, "\nSample record from this project:\n")
				common.DisplayField(os.Stderr, "  DID", sample.Did)
				if sample.FileName != nil && *sample.FileName != "" {
					common.DisplayField(os.Stderr, "  Filename", *sample.FileName)
				}
				if sample.Size != nil {
					common.DisplayField(os.Stderr, "  Size", fmt.Sprintf("%d bytes", *sample.Size))
				}
			} else {
				fmt.Fprintf(os.Stderr, "\nNo records found for this project.\n")
			}

			fmt.Fprintf(os.Stderr, "\nThis will DELETE ALL records in project '%s'.\n", projectId)
			common.DisplayFooter(os.Stderr)

			if err := common.PromptForConfirmation(os.Stderr, fmt.Sprintf("Type the project ID '%s' to confirm deletion", projectId), projectId, true); err != nil {
				return err
			}
		}

		// Delete the matching records
		logger.Debug(fmt.Sprintf("Deleting all records for project %s...", projectId))
		_, err = drsClient.Client.Index().DeleteByQuery(context.Background(), syfonclient.DeleteByQueryOptions{
			Organization: organization,
			ProjectID:    projectId,
		})
		if err != nil {
			return fmt.Errorf("error deleting project %s: %v", projectId, err)
		}

		logger.Debug(fmt.Sprintf("Successfully deleted all records for project %s", projectId))
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVar(&confirmFlag, "confirm", "", "skip interactive confirmation by providing the project_id (e.g., --confirm my-project)")
}
