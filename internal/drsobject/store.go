package drsobject

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func objectPath(basePath string, oid string) (string, error) {
	oid = strings.TrimPrefix(oid, "sha256:")
	if len(oid) != 64 {
		return "", fmt.Errorf("error: %s is not a valid sha256 hash", oid)
	}
	return filepath.Join(basePath, oid[:2], oid[2:4], oid), nil
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
