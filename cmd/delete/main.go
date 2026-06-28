package delete

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var (
	remote      string
	confirmFlag bool
)

const confirmYes = "yes"

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:    "delete <hash-type> <oid>",
	Short:  "Delete a file using hash and file object ID",
	Long:   "Delete a file using file object ID. Use lfs ls-files to get oid",
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		hashType, oid := args[0], args[1]

		// check hash type is valid Checksum type and sha256
		if hashType != hash.ChecksumTypeSHA256.String() {
			return fmt.Errorf("only sha256 supported, you requested to remove: %s", hashType)
		}

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

		// Get record details before deletion for confirmation
		records, err := lookup.ObjectsByHashForScope(context.Background(), drsClient, oid)
		if err != nil {
			return fmt.Errorf("error getting records for OID %s: %v", oid, err)
		}
		if len(records) == 0 {
			return fmt.Errorf("no records found for OID %s", oid)
		}

		// Show details and get confirmation unless --confirm flag is set
		if !confirmFlag {
			projectId := drsClient.ProjectId
			if err := displayWarningHeader(os.Stderr, "DELETE a DRS record"); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "Remote", string(remoteName)); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "Project", projectId); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "OID", oid); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "Hash Type", hashType); err != nil {
				return err
			}
			if err := displayField(os.Stderr, "Matched DIDs", fmt.Sprintf("%d", len(records))); err != nil {
				return err
			}
			if len(records) > 0 {
				if err := displayField(os.Stderr, "Example DID", records[0].Id); err != nil {
					return err
				}
			}
			if err := displayField(os.Stderr, "Warning", "This deletes all DIDs (pointers) resolved by this SHA256 in this backend"); err != nil {
				return err
			}
			if err := displayFooter(os.Stderr); err != nil {
				return err
			}

			if err := promptForConfirmation(
				os.Stderr,
				"Type 'yes' to confirm deletion",
				confirmYes,
				false,
			); err != nil {
				return err
			}
		}

		// Delete the matching record
		err = drsClient.Client.DRS().DeleteRecordsByHash(context.Background(), oid)
		if err != nil {
			return fmt.Errorf("error deleting file for OID %s: %v", oid, err)
		}

		logger.Debug(fmt.Sprintf("Successfully deleted record for OID %s", oid))
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().BoolVar(&confirmFlag, "confirm", false, "skip interactive confirmation prompt")
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
