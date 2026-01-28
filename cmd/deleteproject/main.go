package deleteproject

import (
	"context"
	"fmt"
	"os"

	"github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/utils"
	"github.com/spf13/cobra"
)

var (
	remote      string
	confirmFlag string
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:    "delete-project <project_id>",
	Short:  "Delete all indexd records for a given project",
	Long:   "Delete all indexd records for a given project",
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
			logger.Error(fmt.Sprintf("error creating indexd client: %s", err))
			return err
		}

		// Cast to GitDrsIdxdClient to access GetProjectSample
		indexdClient, ok := drsClient.(*indexd.GitDrsIdxdClient)
		if !ok {
			return fmt.Errorf("client is not an IndexDClient, cannot proceed with delete-project")
		}

		// Get a sample record to show the user what will be deleted
		sampleRecords, err := indexdClient.GetProjectSample(context.Background(), projectId, 1)
		if err != nil {
			return fmt.Errorf("error getting sample records for project %s: %v", projectId, err)
		}

		// Show details and get confirmation unless --confirm flag matches project_id
		if confirmFlag != "" && confirmFlag != projectId {
			return fmt.Errorf("error: --confirm value '%s' does not match project ID '%s'", confirmFlag, projectId)
		}
		if confirmFlag != projectId {
			utils.DisplayWarningHeader(os.Stderr, "DELETE ALL RECORDS for a project")
			utils.DisplayField(os.Stderr, "Remote", string(remoteName))
			utils.DisplayField(os.Stderr, "Project ID", projectId)

			if len(sampleRecords) > 0 {
				sample := sampleRecords[0]
				fmt.Fprintf(os.Stderr, "\nSample record from this project:\n")
				utils.DisplayField(os.Stderr, "  DID", sample.Id)
				if sample.Name != "" {
					utils.DisplayField(os.Stderr, "  Filename", sample.Name)
				}
				utils.DisplayField(os.Stderr, "  Size", fmt.Sprintf("%d bytes", sample.Size))
				if sample.CreatedTime != "" {
					utils.DisplayField(os.Stderr, "  Created", sample.CreatedTime)
				}
			} else {
				fmt.Fprintf(os.Stderr, "\nNo records found for this project.\n")
			}

			fmt.Fprintf(os.Stderr, "\nThis will DELETE ALL records in project '%s'.\n", projectId)
			utils.DisplayFooter(os.Stderr)

			if err := utils.PromptForConfirmation(os.Stderr, fmt.Sprintf("Type the project ID '%s' to confirm deletion", projectId), projectId, true); err != nil {
				return err
			}
		}

		// Delete the matching records
		logger.Debug(fmt.Sprintf("Deleting all records for project %s...", projectId))
		err = drsClient.DeleteRecordsByProject(context.Background(), projectId)
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
