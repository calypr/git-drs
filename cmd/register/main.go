package register

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "register",
	Short: "Register all pending DRS objects with indexd",
	Long:  "Reads pending objects from .drs/lfs/objects/ and registers them with indexd (does not upload files)",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := client.NewLogger("", false)
		if err != nil {
			return err
		}
		defer logger.Close()

		drsClient, err := client.NewIndexDClient(logger)
		if err != nil {
			return fmt.Errorf("error creating indexd client: %v", err)
		}

		// Get all pending objects
		pendingObjects, err := getPendingObjects(logger)
		if err != nil {
			return fmt.Errorf("error reading pending objects: %v", err)
		}

		if len(pendingObjects) == 0 {
			logger.Log("No pending objects to register")
			return nil
		}

		logger.Logf("Found %d pending object(s) to register", len(pendingObjects))

		registeredCount := 0
		skippedCount := 0
		errorCount := 0

		// Register each pending object with indexd
		for _, obj := range pendingObjects {
			logger.Logf("Processing %s (OID: %s)", obj.Path, obj.OID)

			// Read the IndexdRecord from disk
			indexdObj, err := client.DrsInfoFromOid(obj.OID, obj.Path)
			if err != nil {
				logger.Logf("Error reading DRS object for %s: %v", obj.Path, err)
				errorCount++
				continue
			}

			// Check if records with this hash already exist in indexd
			records, err := drsClient.GetObjectsByHash("sha256", obj.OID)
			if err != nil {
				logger.Logf("Error querying indexd for %s: %v", obj.Path, err)
				errorCount++
				continue
			}

			// Check if a record with this exact DID already exists
			alreadyExists := false
			for _, record := range records {
				if record.Did == indexdObj.Did {
					alreadyExists = true
					break
				}
			}

			if alreadyExists {
				logger.Logf("Record for %s (DID: %s) already exists in indexd, skipping", obj.Path, indexdObj.Did)
				skippedCount++
				continue
			}

			// If there's an existing record in the same project with this SHA,
			// reuse its URL so all duplicates point to the same S3 file
			projectId, err := config.GetProjectId()
			if err != nil {
				logger.Logf("Error getting project ID: %v", err)
				errorCount++
				continue
			}

			matchingRecord, err := client.FindMatchingRecord(records, projectId)
			if err != nil {
				logger.Logf("Error finding matching record for project: %v", err)
				errorCount++
				continue
			}

			if matchingRecord != nil && len(matchingRecord.URLs) > 0 {
				// Reuse the URL from the existing record
				logger.Logf("Reusing URL from existing record %s: %s", matchingRecord.Did, matchingRecord.URLs[0])
				indexdObj.URLs = matchingRecord.URLs
			}

			// Register the indexd record
			_, err = drsClient.RegisterIndexdRecord(indexdObj)
			if err != nil {
				logger.Logf("Error registering %s with indexd: %v", obj.Path, err)
				errorCount++
				continue
			}

			logger.Logf("Successfully registered %s with DID %s", obj.Path, indexdObj.Did)
			registeredCount++
		}

		// Summary
		logger.Logf("Registration complete: %d registered, %d skipped, %d errors",
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
func getPendingObjects(logger *client.Logger) ([]PendingObject, error) {
	var objects []PendingObject
	objectsDir := config.DRS_OBJS_PATH

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

		parts := strings.SplitN(rel, string(os.PathSeparator), 4)

		if len(parts) != 4 {
			logger.Logf("Skipping malformed path: %s", path)
			return nil
		}

		// Reconstruct OID from parts: XX + XX + rest_of_sha
		// parts[0] = first 2 chars, parts[1] = next 2 chars, parts[2] = full OID
		oid := parts[2] // GetObjectPath stores full OID in the 3rd directory level
		filePath := parts[3]

		objects = append(objects, PendingObject{
			OID:  oid,
			Path: filePath,
		})

		return nil
	})

	if err != nil {
		return nil, err
	}

	logger.Logf("Found %d pending objects in %s", len(objects), objectsDir)
	return objects, nil
}
