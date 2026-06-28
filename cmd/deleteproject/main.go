package deleteproject

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/remoteruntime"
	syservices "github.com/calypr/syfon/client/services"
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

		drsClient, err := remoteruntime.New(cfg, remoteName, logger)
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
		listResp, err := drsClient.Client.Index().List(context.Background(), syservices.ListRecordsOptions{
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
			if err := displayWarningHeader(os.Stderr, "DELETE ALL RECORDS for a project"); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "Remote", string(remoteName)); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "Project ID", projectId); err != nil {
				return err
			}

			if listResp.Records != nil && len(*listResp.Records) > 0 {
				sample := (*listResp.Records)[0]
				fmt.Fprintf(os.Stderr, "\nSample record from this project:\n")
				if err := displayField(os.Stderr, "  DID", sample.Did); err != nil {
					return err
				}
				if sample.FileName != nil && *sample.FileName != "" {
					if err := displayField(os.Stderr, "  Filename", *sample.FileName); err != nil {
						return err
					}
				}
				if sample.Size != nil {
					if err := displayField(os.Stderr, "  Size", fmt.Sprintf("%d bytes", *sample.Size)); err != nil {
						return err
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "\nNo records found for this project.\n")
			}

			fmt.Fprintf(os.Stderr, "\nThis will DELETE ALL records in project '%s'.\n", projectId)
			if err := displayFooter(os.Stderr); err != nil {
				return err
			}

			if err := promptForConfirmation(os.Stderr, fmt.Sprintf("Type the project ID '%s' to confirm deletion", projectId), projectId, true); err != nil {
				return err
			}
		}

		// Delete the matching records
		logger.Debug(fmt.Sprintf("Deleting all records for project %s...", projectId))
		if _, err := drsClient.Client.Index().DeleteByQuery(context.Background(), syservices.DeleteByQueryOptions{
			Organization: organization,
			ProjectID:    projectId,
		}); err != nil {
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

func promptForConfirmation(w io.Writer, prompt string, expectedResponse string, caseSensitive bool) error {
	if _, err := fmt.Fprintf(w, "%s: ", prompt); err != nil {
		return err
	}

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("error reading confirmation: %v", err)
	}

	response = strings.TrimSpace(response)
	if !caseSensitive {
		response = strings.ToLower(response)
		expectedResponse = strings.ToLower(expectedResponse)
	}

	if response != expectedResponse {
		return fmt.Errorf("operation cancelled: confirmation did not match")
	}

	return nil
}

func displayWarningHeader(w io.Writer, operation string) error {
	_, err := fmt.Fprintf(w, "\nWARNING: You are about to %s\n\n", operation)
	return err
}

func displayField(w io.Writer, key, value string) error {
	_, err := fmt.Fprintf(w, "%-11s %s\n", key+":", value)
	return err
}

func displayFooter(w io.Writer) error {
	_, err := fmt.Fprintf(w, "\nThis action CANNOT be undone.\n\n")
	return err
}
