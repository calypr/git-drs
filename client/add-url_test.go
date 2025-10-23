package client

import (
	"encoding/hex"
	"testing"
)

// TestValidateInputs_ValidInputs tests validation with valid S3 URL and SHA256
func TestValidateInputs_ValidInputs(t *testing.T) {
	tests := []struct {
		name    string
		s3URL   string
		sha256  string
		wantErr bool
	}{
		{
			name:    "valid S3 URL with valid SHA256",
			s3URL:   "s3://my-bucket/path/to/file.bam",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr: false,
		},
		{
			name:    "valid S3 URL with uppercase SHA256",
			s3URL:   "s3://bucket/file.txt",
			sha256:  "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
			wantErr: false,
		},
		{
			name:    "S3 URL with nested path",
			s3URL:   "s3://data-bucket/projects/project1/samples/sample1/file.bam",
			sha256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(tt.s3URL, tt.sha256)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateInputs_InvalidS3URL tests validation with invalid S3 URLs
func TestValidateInputs_InvalidS3URL(t *testing.T) {
	validSHA256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	tests := []struct {
		name    string
		s3URL   string
		wantErr bool
	}{
		{
			name:    "missing s3:// prefix",
			s3URL:   "my-bucket/path/to/file.bam",
			wantErr: true,
		},
		{
			name:    "http URL instead of s3",
			s3URL:   "http://bucket/file.bam",
			wantErr: true,
		},
		{
			name:    "https URL instead of s3",
			s3URL:   "https://bucket/file.bam",
			wantErr: true,
		},
		{
			name:    "empty S3 URL",
			s3URL:   "",
			wantErr: true,
		},
		{
			name:    "s3:// without bucket or path",
			s3URL:   "s3://",
			wantErr: false, // The URL validation only checks for s3:// prefix, bucket validation happens in S3 parsing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(tt.s3URL, validSHA256)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateInputs_InvalidSHA256 tests validation with invalid SHA256 hashes
func TestValidateInputs_InvalidSHA256(t *testing.T) {
	validS3URL := "s3://my-bucket/path/to/file.bam"

	tests := []struct {
		name    string
		sha256  string
		wantErr bool
	}{
		{
			name:    "empty SHA256",
			sha256:  "",
			wantErr: true,
		},
		{
			name:    "SHA256 too short",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85",
			wantErr: true,
		},
		{
			name:    "SHA256 too long",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8551",
			wantErr: true,
		},
		{
			name:    "SHA256 with non-hex characters",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8zz",
			wantErr: true,
		},
		{
			name:    "SHA256 with spaces",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 ",
			wantErr: true,
		},
		{
			name:    "SHA1 hash instead of SHA256",
			sha256:  "da39a3ee5e6b4b0d3255bfef95601890afd80709",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(validS3URL, tt.sha256)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateInputs_SHA256Normalization tests that SHA256 is normalized to lowercase
func TestValidateInputs_SHA256Normalization(t *testing.T) {
	validS3URL := "s3://my-bucket/path/to/file.bam"
	uppercaseSHA256 := "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855"

	// Should not error on uppercase SHA256 (it gets normalized internally)
	err := validateInputs(validS3URL, uppercaseSHA256)
	if err != nil {
		t.Errorf("validateInputs() should accept uppercase SHA256, got error: %v", err)
	}
}

// TestValidateInputs_HexDecodeValidation tests that hex decode is properly validated
func TestValidateInputs_HexDecodeValidation(t *testing.T) {
	validS3URL := "s3://my-bucket/path/to/file.bam"

	// Test valid 64-character hex string
	validHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	err := validateInputs(validS3URL, validHex)
	if err != nil {
		t.Errorf("validateInputs() error = %v, want nil", err)
	}

	// Test that hex.DecodeString is properly checked
	// This has correct length but invalid hex
	invalidHex := "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	err = validateInputs(validS3URL, invalidHex)
	if err == nil {
		t.Errorf("validateInputs() should reject invalid hex, got nil error")
	}
}

// TestValidateInputs_CaseSensitivity tests S3 URL prefix is case-sensitive
func TestValidateInputs_CaseSensitivity(t *testing.T) {
	validSHA256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	tests := []struct {
		name    string
		s3URL   string
		wantErr bool
	}{
		{
			name:    "lowercase s3:// prefix",
			s3URL:   "s3://bucket/file.bam",
			wantErr: false,
		},
		{
			name:    "uppercase S3:// prefix",
			s3URL:   "S3://bucket/file.bam",
			wantErr: true,
		},
		{
			name:    "mixed case S3:// prefix",
			s3URL:   "S3://bucket/file.bam",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(tt.s3URL, validSHA256)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidateInputs_EdgeCases tests edge cases
func TestValidateInputs_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		s3URL   string
		sha256  string
		wantErr bool
	}{
		{
			name:    "both S3 URL and SHA256 empty",
			s3URL:   "",
			sha256:  "",
			wantErr: true,
		},
		{
			name:    "S3 URL with multiple slashes",
			s3URL:   "s3://bucket//path///to////file.bam",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr: false,
		},
		{
			name:    "S3 URL with special characters in path",
			s3URL:   "s3://bucket/path/to/file-name_v1.2.3.bam",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr: false,
		},
		{
			name:    "S3 URL with URL-encoded characters",
			s3URL:   "s3://bucket/path/to/file%20name.bam",
			sha256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(tt.s3URL, tt.sha256)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// BenchmarkValidateInputs benchmarks the validation function
func BenchmarkValidateInputs(b *testing.B) {
	s3URL := "s3://my-bucket/path/to/file.bam"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateInputs(s3URL, sha256)
	}
}

// TestSHA256Validation_AllZeros tests SHA256 with all zeros
func TestSHA256Validation_AllZeros(t *testing.T) {
	s3URL := "s3://bucket/file.bam"
	allZerosSHA256 := "0000000000000000000000000000000000000000000000000000000000000000"

	err := validateInputs(s3URL, allZerosSHA256)
	if err != nil {
		t.Errorf("validateInputs() should accept all zeros SHA256, got error: %v", err)
	}
}

// TestSHA256Validation_AllF tests SHA256 with all F's
func TestSHA256Validation_AllF(t *testing.T) {
	s3URL := "s3://bucket/file.bam"
	allFsSHA256 := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	err := validateInputs(s3URL, allFsSHA256)
	if err != nil {
		t.Errorf("validateInputs() should accept all F's SHA256, got error: %v", err)
	}
}

// TestSHA256HexDecodeString tests that validateInputs properly uses hex.DecodeString
func TestSHA256HexDecodeString(t *testing.T) {
	s3URL := "s3://bucket/file.bam"

	tests := []struct {
		name    string
		sha256  string
		wantErr bool
	}{
		{
			name:    "valid hex string",
			sha256:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantErr: false,
		},
		{
			name:    "invalid hex with space in middle",
			sha256:  "0123456789abcdef0123456789abcde 0123456789abcdef0123456789abcdef",
			wantErr: true,
		},
		{
			name:    "invalid hex with G character",
			sha256:  "0123456789abcdef0123456789abcdefg123456789abcdef0123456789abcdef",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(s3URL, tt.sha256)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInputs() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Double-check the error is due to hex decode by manually testing
			if !tt.wantErr {
				decoded, err := hex.DecodeString(tt.sha256)
				if err != nil {
					t.Errorf("hex.DecodeString() failed unexpectedly: %v", err)
				}
				if len(decoded) != 32 {
					t.Errorf("SHA256 should decode to 32 bytes, got %d", len(decoded))
				}
			}
		})
	}
}
