package hash

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChecksumType_IsValid(t *testing.T) {
	tests := []struct {
		name string
		ct   ChecksumType
		want bool
	}{
		{"Valid SHA256", ChecksumTypeSHA256, true},
		{"Valid SHA512", ChecksumTypeSHA512, true},
		{"Valid SHA1", ChecksumTypeSHA1, true},
		{"Valid MD5", ChecksumTypeMD5, true},
		{"Valid ETag", ChecksumTypeETag, true},
		{"Valid CRC32C", ChecksumTypeCRC32C, true},
		{"Valid Trunc512", ChecksumTypeTrunc512, true},
		{"Invalid Custom", "custom_hash", false},
		{"Invalid Empty", "", false},
		{"Invalid Typo", "sha-256", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ct.IsValid(); got != tt.want {
				t.Errorf("ChecksumType.IsValid() = %v, want %v for type %s", got, tt.want, tt.ct)
			}
		})
	}
}

func TestChecksumType_String(t *testing.T) {
	tests := []struct {
		name string
		ct   ChecksumType
		want string
	}{
		{"SHA256 String", ChecksumTypeSHA256, "sha256"},
		{"MD5 String", ChecksumTypeMD5, "md5"},
		{"Custom String", ChecksumType("custom"), "custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ct.String(); got != tt.want {
				t.Errorf("ChecksumType.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertStringMapToHashInfo(t *testing.T) {
	sha256Val := "c237d6e4b953a78921869e5d76206c7144e0b04b50c000100d021f152d88c2f1"
	md5Val := "0f33166d1f93f6756667a42b10118318"
	sha1Val := "94380b063d89dd7d92131230113f9661c9441113"
	etagVal := "fba640263f350c266cc7e11f759600f1-1"

	tests := []struct {
		name        string
		inputHashes map[string]string
		want        HashInfo
	}{
		{
			name: "All Supported Hashes",
			inputHashes: map[string]string{
				"sha256":      sha256Val,
				"md5":         md5Val,
				"sha1":        sha1Val,
				"etag":        etagVal,
				"sha512":      "512_hash",
				"crc32c":      "crc_hash",
				"trunc512":    "trunc512_hash",
				"unsupported": "some_value",
			},
			want: HashInfo{
				MD5:    md5Val,
				SHA:    sha1Val,
				SHA256: sha256Val,
				SHA512: "512_hash",
				CRC:    "crc_hash",
				ETag:   etagVal,
			},
		},
		{
			name:        "Empty Input Map",
			inputHashes: map[string]string{},
			want:        HashInfo{},
		},
		{
			name: "Only Unsupported Hashes",
			inputHashes: map[string]string{
				"unsupported1": "value1",
				"anotherkey":   "value2",
			},
			want: HashInfo{},
		},
		{
			name: "SHA256 and MD5 Only",
			inputHashes: map[string]string{
				"sha256": sha256Val,
				"md5":    md5Val,
			},
			want: HashInfo{
				MD5:    md5Val,
				SHA256: sha256Val,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertStringMapToHashInfo(tt.inputHashes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertStringMapToHashInfo() got = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConvertHashInfoToMap(t *testing.T) {
	// Example hash values
	sha256Val := "c237d6e4b953a78921869e5d76206c7144e0b04b50c000100d021f152d88c2f1"
	md5Val := "0f33166d1f93f6756667a42b10118318"
	sha1Val := "94380b063d89dd7d92131230113f9661c9441113"

	tests := []struct {
		name   string
		hashes HashInfo
		want   map[string]string
	}{
		{
			name: "All Fields Populated",
			hashes: HashInfo{
				MD5:    md5Val,
				SHA:    sha1Val,
				SHA256: sha256Val,
				SHA512: "512_hash",
				CRC:    "crc_hash",
				ETag:   "etag_hash",
			},
			want: map[string]string{
				"md5":    md5Val,
				"sha":    sha1Val,
				"sha256": sha256Val,
				"sha512": "512_hash",
				"crc":    "crc_hash",
				"etag":   "etag_hash",
			},
		},
		{
			name:   "Empty HashInfo",
			hashes: HashInfo{},
			want:   map[string]string{},
		},
		{
			name: "Partial Fields Populated",
			hashes: HashInfo{
				MD5:    md5Val,
				SHA256: sha256Val,
			},
			want: map[string]string{
				"md5":    md5Val,
				"sha256": sha256Val,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertHashInfoToMap(tt.hashes)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertHashInfoToMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertChecksumsToMap(t *testing.T) {
	tests := []struct {
		name      string
		checksums []Checksum
		want      map[string]string
	}{
		{
			name: "Multiple Checksums",
			checksums: []Checksum{
				{Checksum: "hash_value_1", Type: ChecksumTypeSHA256},
				{Checksum: "hash_value_2", Type: ChecksumTypeMD5},
				{Checksum: "hash_value_3", Type: ChecksumType("custom_type")},
			},
			want: map[string]string{
				"sha256":      "hash_value_1",
				"md5":         "hash_value_2",
				"custom_type": "hash_value_3",
			},
		},
		{
			name:      "Empty Slice",
			checksums: []Checksum{},
			want:      map[string]string{},
		},
		{
			name: "Duplicate Types (Last one wins)",
			checksums: []Checksum{
				{Checksum: "first_value", Type: ChecksumTypeSHA256},
				{Checksum: "second_value", Type: ChecksumTypeSHA256},
			},
			want: map[string]string{
				"sha256": "second_value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertChecksumsToMap(tt.checksums)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertChecksumsToMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertChecksumsToHashInfo(t *testing.T) {
	sha256Val := "c237d6e4b953a78921869e5d76206c7144e0b04b50c000100d021f152d88c2f1"
	md5Val := "0f33166d1f93f6756667a42b10118318"

	tests := []struct {
		name      string
		checksums []Checksum
		want      HashInfo
	}{
		{
			name: "Standard Checksums",
			checksums: []Checksum{
				{Checksum: sha256Val, Type: ChecksumTypeSHA256},
				{Checksum: md5Val, Type: ChecksumTypeMD5},
				{Checksum: "512_val", Type: ChecksumTypeSHA512},
				{Checksum: "etag_val", Type: ChecksumTypeETag},
				{Checksum: "crc_val", Type: ChecksumTypeCRC32C},
				{Checksum: "sha1_val", Type: ChecksumTypeSHA1},
			},
			want: HashInfo{
				MD5:    md5Val,
				SHA:    "sha1_val",
				SHA256: sha256Val,
				SHA512: "512_val",
				CRC:    "crc_val",
				ETag:   "etag_val",
			},
		},
		{
			name: "Includes Unsupported Type",
			checksums: []Checksum{
				{Checksum: sha256Val, Type: ChecksumTypeSHA256},
				{Checksum: "unsupported_val", Type: ChecksumType("unknown_hash")}, // Should be ignored
			},
			want: HashInfo{
				SHA256: sha256Val,
			},
		},
		{
			name:      "Empty Checksums Slice",
			checksums: []Checksum{},
			want:      HashInfo{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConvertChecksumsToHashInfo(tt.checksums)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ConvertChecksumsToHashInfo() got = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestHashInfoUnmarshalJSONChecksumsArray(t *testing.T) {
	payload := []byte(`[
		{"checksum":"8f200381b52333426dcad04771eb18f1","type":"md5"},
		{"checksum":"3d0658efb439683ae9649c6213299219","type":"sha256"}
	]`)

	var got HashInfo
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	want := HashInfo{
		MD5:    "8f200381b52333426dcad04771eb18f1",
		SHA256: "3d0658efb439683ae9649c6213299219",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json.Unmarshal() got = %+v, want %+v", got, want)
	}
}
