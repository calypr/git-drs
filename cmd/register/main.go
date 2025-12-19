package register

import (
	"fmt"

	indexdCl "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

var remote string
var Cmd = &cobra.Command{
	Use:   "register",
	Short: "Register all pending DRS objects with indexd",
	Long:  "Reads pending objects from .drs/lfs/objects/ and registers them with indexd (does not upload files)",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs register --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := drslog.NewLogger("", true)
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		var remoteName config.Remote
		if remote != "" {
			remoteName = config.Remote(remote)
		} else {
			remoteName, err = cfg.GetDefaultRemote()
			if err != nil {
				logger.Printf("Error getting default remote: %v", err)
				return err
			}
		}

		cli, err := cfg.GetRemoteClient(remoteName, logger)
		if err != nil {
			return fmt.Errorf("error creating indexd client: %v", err)
		}
		icli, ok := cli.(*indexdCl.IndexDClient)
		if !ok {
			return fmt.Errorf("remote client is not an *indexdCl.IndexDClient (got %T)", cli)
		}

		// Get all pending objects
		pendingObjects, err := drs.GetPendingObjects(logger)
		if err != nil {
			return fmt.Errorf("error reading pending objects: %v", err)
		}

		if len(pendingObjects) == 0 {
			logger.Println("No pending objects to register")
			return nil
		}

		logger.Printf("Found %d pending object(s) to register", len(pendingObjects))

		registeredCount := 0
		skippedCount := 0
		errorCount := 0

		// Register each pending object with indexd
		for _, obj := range pendingObjects {
			logger.Printf("Processing %s (OID: %s)", obj.Path, obj.OID)

			// Read the IndexdRecord from disk
			indexdObj, err := drsmap.DrsInfoFromOid(obj.OID)
			if err != nil {
				logger.Printf("Error reading DRS object for %s: %v", obj.Path, err)
				errorCount++
				continue
			}

			// Check if records with this hash already exist in indexd
			records, err := cli.GetObjectByHash(&hash.Checksum{Type: "sha256", Checksum: obj.OID})
			if err != nil {
				logger.Printf("Error querying indexd for %s: %v", obj.Path, err)
				errorCount++
				continue
			}

			// Check if a record with this exact DID already exists
			alreadyExists := false
			for _, record := range records {
				if record.Id == indexdObj.Id {
					alreadyExists = true
					break
				}
			}

			if alreadyExists {
				logger.Printf("Record for %s (DID: %s) already exists in indexd, skipping", obj.Path, indexdObj.Id)
				skippedCount++
				continue
			}

			// Register the indexd record
			_, err = icli.RegisterRecord(indexdObj)
			if err != nil {
				logger.Printf("Error registering %s with indexd: %v", obj.Path, err)
				errorCount++
				continue
			}

			logger.Printf("Successfully registered %s with DID %s", obj.Path, indexdObj.Id)
			registeredCount++
		}

		// Summary
		logger.Printf("Registration complete: %d registered, %d skipped, %d errors",
			registeredCount, skippedCount, errorCount)

		if errorCount > 0 {
			return fmt.Errorf("completed with %d error(s)", errorCount)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
}
