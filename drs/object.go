package drs

// ChecksumType represents the digest method used to create the checksum
type ChecksumType string

// IANA Named Information Hash Algorithm Registry values and other common types
const (
	ChecksumTypeSHA1     ChecksumType = "sha1"
	ChecksumTypeSHA256   ChecksumType = "sha256"
	ChecksumTypeSHA512   ChecksumType = "sha512"
	ChecksumTypeMD5      ChecksumType = "md5"
	ChecksumTypeETag     ChecksumType = "etag"
	ChecksumTypeCRC32C   ChecksumType = "crc32c"
	ChecksumTypeTrunc512 ChecksumType = "trunc512"
)

// IsValid checks if the checksum type is a known/recommended value
func (ct ChecksumType) IsValid() bool {
	switch ct {
	case ChecksumTypeSHA256, ChecksumTypeSHA512, ChecksumTypeSHA1, ChecksumTypeMD5,
		ChecksumTypeETag, ChecksumTypeCRC32C, ChecksumTypeTrunc512:
		return true
	default:
		return false
	}
}

// String returns the string representation of the checksum type
func (ct ChecksumType) String() string {
	return string(ct)
}

type Checksum struct {
	Checksum string       `json:"checksum"`
	Type     ChecksumType `json:"type"`
}

type AccessURL struct {
	URL     string   `json:"url"`
	Headers []string `json:"headers"`
}

type Authorizations struct {
	//This structure is not stored in the file system
	Value string `json:"value"`
}

type AccessMethod struct {
	Type           string          `json:"type"`
	AccessURL      AccessURL       `json:"access_url"`
	AccessID       string          `json:"access_id,omitempty"`
	Cloud          string          `json:"cloud,omitempty"`
	Region         string          `json:"region,omitempty"`
	Avalible       string          `json:"available,omitempty"`
	Authorizations *Authorizations `json:"Authorizations,omitempty"`
}

type Contents struct {
}

type DRSPage struct {
	DRSObjects []DRSObject `json:"drs_objects"`
}

type DRSObjectResult struct {
	Object *DRSObject
	Error  error
}

type DRSObject struct {
	Id            string         `json:"id"`
	Name          string         `json:"name"`
	SelfURI       string         `json:"self_uri,omitempty"`
	Size          int64          `json:"size"`
	CreatedTime   string         `json:"created_time,omitempty"`
	UpdatedTime   string         `json:"updated_time,omitempty"`
	Version       string         `json:"version,omitempty"`
	MimeType      string         `json:"mime_type,omitempty"`
	Checksums     []Checksum     `json:"checksums"`
	AccessMethods []AccessMethod `json:"access_methods"`
	Contents      []Contents     `json:"contents,omitempty"`
	Description   string         `json:"description,omitempty"`
	Aliases       []string       `json:"aliases,omitempty"`
}
