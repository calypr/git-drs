package drsobject

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	drsapi "github.com/calypr/syfon/apigen/drs"
)

func objectPath(basePath string, oid string) (string, error) {
	if basePath == "" {
		return "", fmt.Errorf("object base path is required")
	}
	oid = strings.TrimSpace(oid)
	if len(oid) >= len("sha256:") && strings.EqualFold(oid[:len("sha256:")], "sha256:") {
		oid = oid[len("sha256:"):]
	}
	if len(oid) != 64 {
		return "", fmt.Errorf("error: %s is not a valid sha256 hash", oid)
	}
	if _, err := hex.DecodeString(oid); err != nil {
		return "", fmt.Errorf("error: %s is not a valid sha256 hash", oid)
	}
	oid = strings.ToLower(oid)
	basePath = filepath.Clean(basePath)
	objectPath := filepath.Join(basePath, oid[:2], oid[2:4], oid)
	rel, err := filepath.Rel(basePath, objectPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("error: object path escapes base path")
	}
	return objectPath, nil
}

func WriteObject(basePath string, drsObj *drsapi.DrsObject, oid string) error {
	drsObjBytes, err := sonic.ConfigFastest.Marshal(drsObj)
	if err != nil {
		return fmt.Errorf("error marshalling DRS object for oid %s: %v", oid, err)
	}

	drsObjPath, err := objectPath(basePath, oid)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(drsObjPath), 0o755); err != nil {
		return fmt.Errorf("error creating directory for %s: %v", drsObjPath, err)
	}

	if err := os.WriteFile(drsObjPath, drsObjBytes, 0o644); err != nil {
		return fmt.Errorf("error writing %s: %v", drsObjPath, err)
	}
	return nil
}

func ReadObject(basePath string, oid string) (*drsapi.DrsObject, error) {
	path, err := objectPath(basePath, oid)
	if err != nil {
		return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}

	drsObjBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading DRS object for oid %s: %v", oid, err)
	}

	var drsObject drsapi.DrsObject
	if err := sonic.ConfigFastest.Unmarshal(drsObjBytes, &drsObject); err != nil {
		return nil, fmt.Errorf("error unmarshaling DRS object for oid %s: %v", oid, err)
	}

	return &drsObject, nil
}
