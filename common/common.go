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
	"github.com/google/uuid"
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
	if org != "" {
		return "/programs/" + org + "/projects/" + project, nil
	}
	if project == "" {
		return "", fmt.Errorf("error: project ID is empty")
	}
	if !strings.Contains(project, "-") {
		return "/programs/default/projects/" + project, nil
	}
	projectIdArr := strings.SplitN(project, "-", 2)
	return "/programs/" + projectIdArr[0] + "/projects/" + projectIdArr[1], nil
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
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "sha256:")
	return strings.TrimSpace(raw)
}

func StoragePrefix(org, project string) string {
	org = strings.TrimSpace(org)
	project = strings.TrimSpace(project)
	if org == "" {
		return ""
	}
	if project == "" {
		return "programs/" + org
	}
	return "programs/" + org + "/projects/" + project
}

var drsUUIDNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

type ObjectBuilder struct {
	Bucket        string
	ProjectID     string
	Organization  string
	StoragePrefix string
	PathStyle     string
}

func NewObjectBuilder(bucket, projectID string) ObjectBuilder {
	return ObjectBuilder{Bucket: bucket, ProjectID: projectID}
}

func (b ObjectBuilder) Build(fileName string, checksum string, size int64, drsID string) (*drsapi.DrsObject, error) {
	prefix := strings.Trim(strings.TrimSpace(b.StoragePrefix), "/")
	if prefix == "" {
		prefix = StoragePrefix(b.Organization, b.ProjectID)
	}
	return BuildDrsObjWithPrefix(fileName, checksum, size, drsID, b.Bucket, b.Organization, b.ProjectID, prefix)
}

func BuildDrsObjWithPrefix(fileName string, checksum string, size int64, drsID string, bucket string, org string, projectID string, prefix string) (*drsapi.DrsObject, error) {
	checksum = NormalizeOid(checksum)
	if checksum == "" {
		return nil, fmt.Errorf("checksum is required")
	}

	obj := &drsapi.DrsObject{
		Id:      drsID,
		SelfUri: "drs://" + drsID,
		Size:    size,
		Name:    &fileName,
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: checksum},
		},
	}

	if bucket == "" {
		return obj, nil
	}

	resourcePath, err := ProjectToResource(org, projectID)
	if err != nil {
		return nil, err
	}

	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = StoragePrefix(org, projectID)
	}

	accessURL := fmt.Sprintf("s3://%s/%s", bucket, checksum)
	if prefix != "" {
		accessURL = fmt.Sprintf("s3://%s/%s/%s", bucket, prefix, checksum)
	}

	am := drsapi.AccessMethod{
		Type: drsapi.AccessMethodTypeS3,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: accessURL},
	}
	if resourcePath != "" {
		issuers := []string{resourcePath}
		am.Authorizations = &struct {
			BearerAuthIssuers   *[]string                                          `json:"bearer_auth_issuers,omitempty"`
			DrsObjectId         *string                                            `json:"drs_object_id,omitempty"`
			PassportAuthIssuers *[]string                                          `json:"passport_auth_issuers,omitempty"`
			SupportedTypes      *[]drsapi.AccessMethodAuthorizationsSupportedTypes `json:"supported_types,omitempty"`
		}{BearerAuthIssuers: &issuers}
	}
	ams := []drsapi.AccessMethod{am}
	obj.AccessMethods = &ams
	return obj, nil
}

func ConvertToCandidate(obj *drsapi.DrsObject) drsapi.DrsObjectCandidate {
	if obj == nil {
		return drsapi.DrsObjectCandidate{}
	}
	return drsapi.DrsObjectCandidate{
		AccessMethods: obj.AccessMethods,
		Aliases:       obj.Aliases,
		Checksums:     obj.Checksums,
		Contents:      obj.Contents,
		Description:   obj.Description,
		MimeType:      obj.MimeType,
		Name:          obj.Name,
		Size:          obj.Size,
		Version:       obj.Version,
	}
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
