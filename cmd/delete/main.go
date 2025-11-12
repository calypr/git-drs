package delete

import (
	"fmt"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/spf13/cobra"
)

var (
	server  string
	dstPath string
	drsObj  *drs.DRSObject
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

		// check hash type is valid Checksum type
		if !drs.ChecksumType(hashType).IsValid() {
			return fmt.Errorf("invalid hash type: %s", hashType)
		}

		logger, err := client.NewLogger("", true)
		if err != nil {
			return err
		}
		defer logger.Close()

		indexdClient, err := client.NewIndexDClient(logger)
		if err != nil {
			logger.Logf("error creating indexd client: %s", err)
			return err
		}
		// get records by hash
		records, err := indexdClient.GetObjectsByHash(hashType, oid)
		if err != nil {
			return fmt.Errorf("Error getting records for OID %s: %v", oid, err)
		}
		if len(records) == 0 {
			return fmt.Errorf("No records found for OID %s", oid)
		}

		// Get project ID from config to find matching record
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("Error loading config: %v", err)
		}
		if cfg.Servers.Gen3 == nil || cfg.Servers.Gen3.Auth.ProjectID == "" {
			return fmt.Errorf("No project ID found in config")
		}

		// Find a record that matches the project ID
		matchingRecord, err := client.FindMatchingRecord(records, cfg.Servers.Gen3.Auth.ProjectID)
		if err != nil {
			return fmt.Errorf("Error finding matching record for project %s: %v", cfg.Servers.Gen3.Auth.ProjectID, err)
		}
		if matchingRecord == nil {
			return fmt.Errorf("No matching record found for project %s", cfg.Servers.Gen3.Auth.ProjectID)
		}

		// Delete the matching record
		err = indexdClient.DeleteIndexdRecord(matchingRecord.Did)
		if err != nil {
			return fmt.Errorf("Error deleting file for OID %s: %v", oid, err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
