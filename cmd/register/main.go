package register

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/projectdir"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "register",
	Short: "Register all pending DRS objects with indexd",
	Long:  "Reads pending objects from .drs/lfs/objects/ and registers them with indexd (does not upload files)",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := drslog.NewLogger("", true)
		if err != nil {
			return err
		}

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		cli, err := cfg.GetCurrentRemoteClient(logger)
		if err != nil {
			return fmt.Errorf("error creating indexd client: %v", err)
		}
		icli, _ := cli.(*indexd_client.IndexDClient)

		// Get all pending objects
		pendingObjects, err := getPendingObjects(logger)
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
			records, err := cli.GetObjectsByHash(&drs.Checksum{Type: "sha256", Checksum: obj.OID})
			if err != nil {
				logger.Printf("Error querying indexd for %s: %v", obj.Path, err)
				errorCount++
				continue
			}

			// Check if a record with this exact DID already exists
			alreadyExists := false
			for _, record := range records[0] {
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

type PendingObject struct {
	OID  string
	Path string
}

// getPendingObjects walks .drs/lfs/objects/ to find all pending records
func getPendingObjects(logger *log.Logger) ([]PendingObject, error) {
	var objects []PendingObject
	objectsDir := projectdir.DRS_OBJS_PATH

	if _, err := os.Stat(objectsDir); os.IsNotExist(err) {
		return objects, nil // No pending objects
	}

	err := filepath.Walk(objectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Parse the path structure: .drs/lfs/objects/XX/XX/FULL_SHA/filepath
		rel, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return err
		}

		parts := strings.SplitN(rel, string(os.PathSeparator), 3)

		if len(parts) != 3 {
			logger.Printf("Skipping malformed path: %s", path)
			return nil
		}

		// Reconstruct OID from parts: XX + XX + rest_of_sha
		// parts[0] = first 2 chars, parts[1] = next 2 chars, parts[2] = full OID
		oid := parts[2] // GetObjectPath stores full OID in the 3rd directory level

		objects = append(objects, PendingObject{
			OID: oid,
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	logger.Printf("Found %d pending objects in %s", len(objects), objectsDir)
	return objects, nil
}
