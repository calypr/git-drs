package utils

import (
	"fmt"
	"path/filepath"
)

var (
	ANVIL_ENDPOINT string = "https://drshub.dsde-prod.broadinstitute.org/api/v4/drs/resolve"
	DRS_REF_DIR    string = ".git/drs/objects"
	DRS_LOG_FILE   string = ".drs/anvil.log"
)

// CreateCustomPath creates a custom path based on the DRS URI
// For example, DRS URI drs://<namespace>:<drs_id>
// create custom path <baseDir>/<namespace>/<drs_id>
func CreateCustomPath(baseDir, drsURI string) (string, error) {
	const prefix = "drs://"
	if len(drsURI) <= len(prefix) || drsURI[:len(prefix)] != prefix {
		return "", fmt.Errorf("invalid DRS URI: %s", drsURI)
	}
	rest := drsURI[len(prefix):]

	// Split by first colon
	colonIdx := -1
	for i, c := range rest {
		if c == ':' {
			colonIdx = i
			break
		}
	}
	if colonIdx == -1 {
		return "", fmt.Errorf("DRS URI missing colon: %s", drsURI)
	}
	namespace := rest[:colonIdx]
	drsId := rest[colonIdx+1:]
	return filepath.Join(baseDir, namespace, drsId), nil
}
