package lfs

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/git-drs/common"
)

// This file contains functions that pertain to .git/drs/lfs/objects directory walk
type PendingObject struct {
	OID  string
	Path string
}

// getPendingObjects walks .git/drs/lfs/objects/ to find all pending records
func GetPendingObjects(logger *slog.Logger) ([]*PendingObject, error) {
	var objects []*PendingObject
	objectsDir := common.DRS_OBJS_PATH

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
			logger.Debug(fmt.Sprintf("Skipping malformed path: %s", path))
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
	logger.Debug(fmt.Sprintf("Found %d pending objects in %s", len(objects), objectsDir))
	return objects, nil
}

func GetDrsLfsObjects(logger *slog.Logger) (map[string]*drs.DRSObject, error) {
	objects := map[string]*drs.DRSObject{}
	objectsDir := common.DRS_OBJS_PATH
	if _, err := os.Stat(objectsDir); os.IsNotExist(err) {
		logger.Debug(fmt.Sprintf("DRS objects directory not found: %s", objectsDir))
		return nil, nil
	}

	err := filepath.Walk(objectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Error(fmt.Sprintf("Error accessing path %s: %v", path, err))
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
			logger.Debug(fmt.Sprintf("Skipping malformed path: %s", path))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			logger.Error(fmt.Sprintf("Error reading file %s: %v", path, err))
			return err
		}
		var drsObject drs.DRSObject
		if err := sonic.ConfigFastest.Unmarshal(data, &drsObject); err != nil {
			logger.Error(fmt.Sprintf("Error unmarshalling JSON from %s: %v", path, err))
			return nil
		}

		// This could be problematic
		if drsObject.Checksums.SHA256 != "" {
			objects[drsObject.Checksums.SHA256] = &drsObject
		}

		logger.Debug(fmt.Sprintf("Successfully unmarshaled DRSObject from %s:\n%+v", path, drsObject))
		return nil
	})
	if err != nil {
		return nil, err
	}
	logger.Debug(fmt.Sprintf("Found and unmarshaled %d DRS objects.", len(objects)))
	return objects, nil
}
