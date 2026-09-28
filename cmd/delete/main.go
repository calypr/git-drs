package delete

import (
	"context"
	"fmt"
	"os"

	"github.com/calypr/git-drs/cmd/internal/confirm"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	"github.com/calypr/syfon/client/apierror"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var (
	remote      string
	confirmFlag bool
)

const confirmYes = "yes"

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
			if err := confirm.WarningHeader(os.Stderr, "DELETE a DRS record"); err != nil {
				return err
			}
			if err := confirm.Field(os.Stderr, "Remote", string(remoteName)); err != nil {
				return err
			}
			if err := confirm.Field(os.Stderr, "Project", projectId); err != nil {
				return err
			}
			if err := confirm.Field(os.Stderr, "OID", oid); err != nil {
				return err
			}
			if err := confirm.Field(os.Stderr, "Hash Type", hashType); err != nil {
				return err
			}
			if err := confirm.Field(os.Stderr, "Matched DIDs", fmt.Sprintf("%d", len(records))); err != nil {
				return err
			}
			if len(records) > 0 {
				if err := confirm.Field(os.Stderr, "Example DID", records[0].Id); err != nil {
					return err
				}
			}
			if err := confirm.Field(os.Stderr, "Warning", "This deletes all DIDs (pointers) resolved by this SHA256 in this backend"); err != nil {
				return err
			}
			if err := confirm.Footer(os.Stderr); err != nil {
				return err
			}

			if err := confirm.Prompt(
				os.Stderr,
				os.Stdin,
				"Type 'yes' to confirm deletion",
				confirmYes,
				false,
			); err != nil {
				return err
			}
		}

		err = deleteByHash(context.Background(), drsClient, oid)
		if err != nil {
			return fmt.Errorf("error deleting file for OID %s: %v", oid, err)
		}

		logger.Debug(fmt.Sprintf("Successfully deleted record for OID %s", oid))
		return nil
	},
}

func deleteByHash(ctx context.Context, drsClient *remoteruntime.GitContext, oid string) error {
	if drsClient == nil || drsClient.Client == nil {
		return fmt.Errorf("DRS client unavailable")
	}
	response, err := drsClient.Client.InternalAPI().InternalBulkDeleteHashesWithResponse(ctx, internalapi.BulkHashesRequest{
		Hashes: []string{oid},
	})
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("bulk hash delete returned no response")
	}
	if response.JSON200 == nil {
		return apierror.FromResponse(response.HTTPResponse, response.Body)
	}
	return nil
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().BoolVar(&confirmFlag, "confirm", false, "skip interactive confirmation prompt")
}
