package drs

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/projectdir"
)

// This file contains functions that pertain to .drs/lfs/objects directory walk
type PendingObject struct {
	OID  string
	Path string
}

// getPendingObjects walks .drs/lfs/objects/ to find all pending records
func GetPendingObjects(logger *log.Logger) ([]*PendingObject, error) {
	var objects []*PendingObject
	objectsDir := projectdir.DRS_OBJS_PATH

	if _, err := os.Stat(objectsDir); os.IsNotExist(err) {
		return nil, nil
	}
	err := filepath.Walk(objectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return err
		}
		parts := strings.SplitN(rel, string(os.PathSeparator), 3)
		if len(parts) != 3 {
			logger.Printf("Skipping malformed path: %s", path)
			return nil
		}
		oid := parts[2] // GetObjectPath stores full OID in the 3rd directory level
		objects = append(objects, &PendingObject{
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

func GetDrsLfsObjects(logger *log.Logger) ([]*DRSObject, error) {
	var objects []*DRSObject
	objectsDir := projectdir.DRS_OBJS_PATH
	if _, err := os.Stat(objectsDir); os.IsNotExist(err) {
		logger.Printf("DRS objects directory not found: %s", objectsDir)
		return nil, nil
	}

	err := filepath.Walk(objectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Printf("Error accessing path %s: %v", path, err)
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Printf("Error reading file %s: %v", path, err)
			return err
		}
		var drsObject DRSObject
		if err := json.Unmarshal(data, &drsObject); err != nil {
			logger.Printf("Error unmarshalling JSON from %s: %v", path, err)
			return nil
		}
		objects = append(objects, &drsObject)
		logger.Printf("Successfully unmarshaled DRSObject from %s:\n%+v", path, drsObject)
		return nil
	})
	if err != nil {
		return nil, err
	}
	logger.Printf("Found and unmarshaled %d DRS objects.", len(objects))
	return objects, nil
}
