package delete

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/log"
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

		logger, err := log.NewLogger("", true)
		if err != nil {
			return err
		}
		defer logger.Close()

		config, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		drsClient, err := config.GetCurrentRemoteClient(logger)
		if err != nil {
			logger.Logf("error creating indexd client: %s", err)
			return err
		}
		// get records by hash
		records, err := drsClient.GetObjectsByHash(hashType, oid)
		if err != nil {
			return fmt.Errorf("Error getting records for OID %s: %v", oid, err)
		}
		if len(records) == 0 {
			return fmt.Errorf("No records found for OID %s", oid)
		}

		// Find a record that matches the project ID
		matchingRecord, err := drsmap.FindMatchingRecord(records, drsClient.GetProjectId())
		if err != nil {
			return fmt.Errorf("Error finding matching record for project %s: %v", drsClient.GetProjectId(), err)
		}
		if matchingRecord == nil {
			return fmt.Errorf("No matching record found for project %s", drsClient.GetProjectId())
		}

		// Delete the matching record
		err = drsClient.DeleteRecord(matchingRecord.Id)
		if err != nil {
			return fmt.Errorf("Error deleting file for OID %s: %v", oid, err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
