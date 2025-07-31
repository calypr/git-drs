package client

// HashInfo represents file hash information as per OpenAPI spec
// Patterns are documented for reference, but not enforced at struct level
// md5:    ^[0-9a-f]{32}$
// sha:    ^[0-9a-f]{40}$
// sha256: ^[0-9a-f]{64}$
// sha512: ^[0-9a-f]{128}$
// crc:    ^[0-9a-f]{8}$
// etag:   ^[0-9a-f]{32}(-\d+)?$
type HashInfo struct {
	MD5    string `json:"md5,omitempty"`
	SHA    string `json:"sha,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	SHA512 string `json:"sha512,omitempty"`
	CRC    string `json:"crc,omitempty"`
	ETag   string `json:"etag,omitempty"`
}

// subset of the OpenAPI spec for the InputInfo object in indexd
// https://github.com/uc-cdis/indexd/blob/master/openapis/swagger.yaml
// TODO: make another object based on VersionInputInfo that has content_created_date and so can handle a POST of dates via indexd/<GUID>
type IndexdRecord struct {
	// Unique identifier for the record (UUID)
	Did string `json:"did"`

	// Human-readable file name
	FileName string `json:"file_name,omitempty"`

	// List of URLs where the file can be accessed
	URLs []string `json:"urls"`

	// Hashes of the file (e.g., md5, sha256)
	Size int64 `json:"size"`

	// List of access control lists (ACLs)
	ACL []string `json:"acl,omitempty"`

	// List of authorization policies
	Authz []string `json:"authz,omitempty"`

	Hashes HashInfo `json:"hashes,omitzero"`

	// Additional metadata as key-value pairs
	Metadata map[string]string `json:"metadata,omitempty"`

	// Version of the record (optional)
	Version string `json:"version,omitempty"`

	// // Created timestamp (RFC3339 format)
	// ContentCreatedDate string `json:"content_created_date,omitempty"`

	// // Updated timestamp (RFC3339 format)
	// ContentUpdatedDate string `json:"content_updated_date,omitempty"`
}

type ListRecords struct {
	IDs      []string       `json:"ids"`
	Records  []OutputInfo   `json:"records"`
	Size     int64          `json:"size"`
	Start    int64          `json:"start"`
	Limit    int64          `json:"limit"`
	FileName string         `json:"file_name"`
	URLs     []string       `json:"urls"`
	ACL      []string       `json:"acl"`
	Authz    []string       `json:"authz"`
	Hashes   HashInfo       `json:"hashes"`
	Metadata map[string]any `json:"metadata"`
	Version  string         `json:"version"`
}

type OutputInfo struct {
	Did          string         `json:"did"`
	BaseID       string         `json:"baseid"`
	Rev          string         `json:"rev"`
	Form         string         `json:"form"`
	Size         int64          `json:"size"`
	FileName     string         `json:"file_name"`
	Version      string         `json:"version"`
	Uploader     string         `json:"uploader"`
	URLs         []string       `json:"urls"`
	ACL          []string       `json:"acl"`
	Authz        []string       `json:"authz"`
	Hashes       HashInfo       `json:"hashes"`
	UpdatedDate  string         `json:"updated_date"`
	CreatedDate  string         `json:"created_date"`
	Metadata     map[string]any `json:"metadata"`
	URLsMetadata map[string]any `json:"urls_metadata"`
}
