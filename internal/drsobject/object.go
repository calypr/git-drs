package drsobject

import (
	"fmt"
	"net/url"
	"strings"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syfoncommon "github.com/calypr/syfon/common"
	"github.com/google/uuid"
)

// UUIDNamespace is the legacy deterministic UUID namespace used to derive
// local DRS IDs from "<projectID>:<sha256>". It intentionally uses the DNS
// namespace UUID value because existing deployed git-drs records were generated
// with this exact namespace. Do not change it without a DRS ID migration plan.
var UUIDNamespace = uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")

func NormalizeChecksum(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "sha256:")
	return strings.TrimSpace(raw)
}

func NormalizeOid(raw string) string {
	return NormalizeChecksum(raw)
}

type Builder struct {
	Bucket        string
	Project       string
	Organization  string
	StoragePrefix string
	Provider      string
	AccessScheme  string
}

func NewBuilder(bucket, project string) Builder {
	return Builder{Bucket: bucket, Project: project}
}

func (b Builder) Build(fileName string, checksum string, size int64, drsID string) (*drsapi.DrsObject, error) {
	prefix := strings.Trim(strings.TrimSpace(b.StoragePrefix), "/")
	return BuildWithPrefix(fileName, checksum, size, drsID, b.Bucket, b.Organization, b.Project, prefix)
}

func BuildWithPrefix(fileName string, checksum string, size int64, drsID string, bucket string, org string, project string, prefix string) (*drsapi.DrsObject, error) {
	return BuildWithOptions(fileName, checksum, size, drsID, LocationOptions{
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

type LocationOptions struct {
	Bucket        string
	Organization  string
	Project       string
	StoragePrefix string
	Provider      string
	AccessScheme  string
}

func BuildWithOptions(fileName string, checksum string, size int64, drsID string, opts LocationOptions) (*drsapi.DrsObject, error) {
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
	if authzMap := syfoncommon.AuthzMapFromScope(opts.Organization, opts.Project); authzMap != nil {
		am.Authorizations = syfoncommon.AccessMethodAuthorizationsFromAuthzMap(authzMap)
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
