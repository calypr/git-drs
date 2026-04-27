package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/bytedance/sonic"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
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

// ProjectToResource converts a project ID and optional organization to a GA4GH resource path.
// Kept for internal use only; the DRS wire format now uses AuthzMapFromOrgProject.
func ProjectToResource(org, project string) (string, error) {
	if org != "" {
		return "/programs/" + org + "/projects/" + project, nil
	}
	if project == "" {
		return "", fmt.Errorf("project ID is empty")
	}
	if !strings.Contains(project, "-") {
		return "/programs/default/projects/" + project, nil
	}
	projectIDParts := strings.SplitN(project, "-", 2)
	return "/programs/" + projectIDParts[0] + "/projects/" + projectIDParts[1], nil
}

// ParseOrgProject resolves the effective org and project from the conventional
// arguments. When org is provided it is used directly. When org is empty the
// project string is split on the first "-" to derive org and project (the same
// convention that ProjectToResource applies).
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

// AuthzMapFromOrgProject builds the wire-format authorizations map from an
// org and project. An empty project means org-wide access.
func AuthzMapFromOrgProject(org, project string) map[string][]string {
	org = strings.TrimSpace(org)
	if org == "" {
		return nil
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return map[string][]string{org: {}}
	}
	return map[string][]string{org: {project}}
}

// AuthzMatchesScope reports whether the authz map grants access for the given
// org and project. An empty project list in the map means org-wide access.
func AuthzMatchesScope(authzMap map[string][]string, org, project string) bool {
	if len(authzMap) == 0 || org == "" {
		return false
	}
	projects, ok := authzMap[org]
	if !ok {
		return false
	}
	if len(projects) == 0 {
		return true // org-wide
	}
	for _, p := range projects {
		if p == project {
			return true
		}
	}
	return false
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

func NormalizeChecksum(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "sha256:")
	return strings.TrimSpace(raw)
}

func NormalizeOid(raw string) string {
	return NormalizeChecksum(raw)
}

func StoragePrefix(org, project string) string {
	_ = org
	_ = project
	return ""
}

type ObjectBuilder struct {
	Bucket        string
	Project       string
	Organization  string
	StoragePrefix string
	Provider      string
	AccessScheme  string
}

func NewObjectBuilder(bucket, project string) ObjectBuilder {
	return ObjectBuilder{Bucket: bucket, Project: project}
}

func (b ObjectBuilder) Build(fileName string, checksum string, size int64, drsID string) (*drsapi.DrsObject, error) {
	prefix := strings.Trim(strings.TrimSpace(b.StoragePrefix), "/")
	return BuildDrsObjWithPrefix(fileName, checksum, size, drsID, b.Bucket, b.Organization, b.Project, prefix)
}

func BuildDrsObjWithPrefix(fileName string, checksum string, size int64, drsID string, bucket string, org string, project string, prefix string) (*drsapi.DrsObject, error) {
	return BuildDrsObjWithOptions(fileName, checksum, size, drsID, ObjectLocationOptions{
		Bucket:        bucket,
		Organization:  org,
		Project:       project,
		StoragePrefix: prefix,
	})
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

type ObjectLocationOptions struct {
	Bucket        string
	Organization  string
	Project       string
	StoragePrefix string
	Provider      string
	AccessScheme  string
}

func BuildDrsObjWithOptions(fileName string, checksum string, size int64, drsID string, opts ObjectLocationOptions) (*drsapi.DrsObject, error) {
	checksum = NormalizeChecksum(checksum)
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

	if opts.Bucket == "" {
		return obj, nil
	}

	prefix := strings.Trim(strings.TrimSpace(opts.StoragePrefix), "/")

	accessURL, methodType, err := BuildAccessURL(opts.Bucket, prefix, checksum, opts.Provider, opts.AccessScheme)
	if err != nil {
		return nil, err
	}

	am := drsapi.AccessMethod{
		Type: drsapi.AccessMethodType(methodType),
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: accessURL},
	}
	if authzMap := AuthzMapFromOrgProject(opts.Organization, opts.Project); authzMap != nil {
		am.Authorizations = &authzMap
	}
	ams := []drsapi.AccessMethod{am}
	obj.AccessMethods = &ams
	return obj, nil
}

func BuildAccessURL(bucket string, prefix string, key string, provider string, accessScheme string) (string, string, error) {
	bucket = strings.TrimSpace(bucket)
	key = strings.Trim(strings.TrimSpace(key), "/")
	if bucket == "" {
		return "", "", fmt.Errorf("bucket is required")
	}
	if key == "" {
		return "", "", fmt.Errorf("key is required")
	}

	scheme := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(accessScheme)), "://")
	if scheme == "" {
		scheme = schemeFromProvider(provider)
	}

	if u, err := url.Parse(bucket); err == nil && u.Scheme != "" {
		scheme = strings.ToLower(u.Scheme)
		bucket = u.Host
		basePath := strings.Trim(u.Path, "/")
		if basePath != "" {
			if prefix == "" {
				prefix = basePath
			} else {
				prefix = strings.Trim(basePath+"/"+strings.Trim(prefix, "/"), "/")
			}
		}
	}

	if scheme == "" {
		scheme = "s3"
	}
	methodType := accessMethodTypeForScheme(scheme)
	path := key
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix != "" {
		path = prefix + "/" + key
	}
	return fmt.Sprintf("%s://%s/%s", scheme, bucket, path), methodType, nil
}

func schemeFromProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "s3", "aws", "minio":
		return "s3"
	case "gcs", "gcp", "google", "gs":
		return "gs"
	case "azure", "azblob", "blob":
		return "az"
	default:
		return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(provider)), "://")
	}
}

func accessMethodTypeForScheme(scheme string) string {
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(scheme), "://")) {
	case "gcs", "gs":
		return string(drsapi.AccessMethodTypeGs)
	case "s3":
		return string(drsapi.AccessMethodTypeS3)
	default:
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(scheme), "://"))
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
