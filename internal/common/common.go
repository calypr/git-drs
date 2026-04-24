package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/bytedance/sonic"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/drsmeta"
)

// AddUnique appends items from 'toAdd' to 'existing' only if they're not already present.
// Returns the updated slice with unique items.
func AddUnique[T comparable](existing []T, toAdd []T) []T {
	// seen map uses struct{} as the value for memory efficiency
	seen := make(map[T]struct{}, len(existing))

	// Populate the set with existing items
	for _, item := range existing {
		seen[item] = struct{}{}
	}

	for _, item := range toAdd {
		// check if item not yet in the set
		if _, found := seen[item]; !found {
			existing = append(existing, item)
			// Add the new unique item to the set
			seen[item] = struct{}{}
		}
	}
	return existing
}

// ProjectToResource converts a project ID (and optional organization) to a GA4GH resource path.
// If org is provided, it returns /programs/{org}/projects/{project}.
// If org is empty, it expects project to be in "program-project" format and splits it.
func ProjectToResource(org, project string) (string, error) {
	return drsmeta.ProjectToResource(org, project)
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

func NormalizeOid(raw string) string {
	return drsmeta.NormalizeChecksum(raw)
}

func StoragePrefix(org, project string) string {
	return drsmeta.StoragePrefix(org, project)
}

type ObjectBuilder struct {
	Bucket        string
	ProjectID     string
	Organization  string
	StoragePrefix string
	Provider      string
	AccessScheme  string
	PathStyle     string
}

func NewObjectBuilder(bucket, projectID string) ObjectBuilder {
	return ObjectBuilder{Bucket: bucket, ProjectID: projectID}
}

func (b ObjectBuilder) Build(fileName string, checksum string, size int64, drsID string) (*drsapi.DrsObject, error) {
	return drsmeta.ObjectBuilder{
		Bucket:        b.Bucket,
		ProjectID:     b.ProjectID,
		Organization:  b.Organization,
		StoragePrefix: b.StoragePrefix,
		Provider:      b.Provider,
		AccessScheme:  b.AccessScheme,
		PathStyle:     b.PathStyle,
	}.Build(fileName, checksum, size, drsID)
}

func BuildDrsObjWithPrefix(fileName string, checksum string, size int64, drsID string, bucket string, org string, projectID string, prefix string) (*drsapi.DrsObject, error) {
	return drsmeta.BuildDrsObjWithPrefix(fileName, checksum, size, drsID, bucket, org, projectID, prefix)
}

func ConvertToCandidate(obj *drsapi.DrsObject) drsapi.DrsObjectCandidate {
	return drsmeta.ConvertToCandidate(obj)
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
