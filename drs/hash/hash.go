package hash

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

var SupportedChecksums = map[string]bool{
	string(ChecksumTypeSHA1):     true,
	string(ChecksumTypeSHA256):   true,
	string(ChecksumTypeSHA512):   true,
	string(ChecksumTypeMD5):      true,
	string(ChecksumTypeETag):     true,
	string(ChecksumTypeCRC32C):   true,
	string(ChecksumTypeTrunc512): true,
}

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

func ConvertStringMapToHashInfo(inputHashes map[string]string) HashInfo {
	hashInfo := HashInfo{}

	for key, value := range inputHashes {
		if !SupportedChecksums[key] {
			continue // Disregard unsupported types
		}
		// We use the string key directly in the switch statement.
		switch key {
		case string(ChecksumTypeMD5):
			hashInfo.MD5 = value
		case string(ChecksumTypeSHA1):
			hashInfo.SHA = value // Maps to SHA field
		case string(ChecksumTypeSHA256):
			hashInfo.SHA256 = value
		case string(ChecksumTypeSHA512):
			hashInfo.SHA512 = value
		case string(ChecksumTypeCRC32C):
			hashInfo.CRC = value // Maps to CRC field
		case string(ChecksumTypeETag):
			hashInfo.ETag = value
		default:
		}
	}

	return hashInfo
}

// convertHashInfoToMap converts HashInfo struct to map[string]string
func ConvertHashInfoToMap(hashes HashInfo) map[string]string {
	result := make(map[string]string)
	if hashes.MD5 != "" {
		result["md5"] = hashes.MD5
	}
	if hashes.SHA != "" {
		result["sha"] = hashes.SHA
	}
	if hashes.SHA256 != "" {
		result["sha256"] = hashes.SHA256
	}
	if hashes.SHA512 != "" {
		result["sha512"] = hashes.SHA512
	}
	if hashes.CRC != "" {
		result["crc"] = hashes.CRC
	}
	if hashes.ETag != "" {
		result["etag"] = hashes.ETag
	}
	return result
}
