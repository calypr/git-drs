package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bytedance/sonic"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

// ParseOrgProject resolves the effective org and project from the conventional
// arguments. When org is provided it is used directly. When org is empty the
// project string is split on the first "-" to derive org and project.
func ParseOrgProject(org, project string) (string, string) {
	if org != "" {
		return org, project
	}
	if project == "" {
		return "", ""
	}
	if !strings.Contains(project, "-") {
		return "default", project
	}
	parts := strings.SplitN(project, "-", 2)
	return parts[0], parts[1]
}

// CalculateFileSHA256 returns the lowercase hex SHA256 checksum for a file.
func CalculateFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// PrintDRSObject marshals and prints a DRS object as JSON.
func PrintDRSObject(obj drsapi.DrsObject, pretty bool) error {
	var out []byte
	var err error

	if pretty {
		out, err = sonic.ConfigFastest.MarshalIndent(obj, "", "  ")
	} else {
		out, err = sonic.ConfigFastest.Marshal(obj)
	}
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", string(out))
	return nil
}
