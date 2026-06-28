package delete

import (
	"context"
	"fmt"
	"os"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/confirm"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drslookup"
	"github.com/calypr/git-drs/internal/drspaths"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var (
	remote      string
	confirmFlag bool
)

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
		records, err := drslookup.ObjectsByHashForScope(context.Background(), drsClient, oid)
		if err != nil {
			return fmt.Errorf("error getting records for OID %s: %v", oid, err)
		}
		if len(records) == 0 {
			return fmt.Errorf("no records found for OID %s", oid)
		}

		// Show details and get confirmation unless --confirm flag is set
		if !confirmFlag {
			projectId := drsClient.ProjectId
			confirm.DisplayWarningHeader(os.Stderr, "DELETE a DRS record")
			confirm.DisplayField(os.Stderr, "Remote", string(remoteName))
			confirm.DisplayField(os.Stderr, "Project", projectId)
			confirm.DisplayField(os.Stderr, "OID", oid)
			confirm.DisplayField(os.Stderr, "Hash Type", hashType)
			confirm.DisplayField(os.Stderr, "Matched DIDs", fmt.Sprintf("%d", len(records)))
			if len(records) > 0 {
				confirm.DisplayField(os.Stderr, "Example DID", records[0].Id)
			}
			confirm.DisplayField(os.Stderr, "Warning", "This deletes all DIDs (pointers) resolved by this SHA256 in this backend")
			confirm.DisplayFooter(os.Stderr)

			if err := confirm.PromptForConfirmation(
				os.Stderr,
				"Type 'yes' to confirm deletion",
				drspaths.ConfirmationYes,
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
