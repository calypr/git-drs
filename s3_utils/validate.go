package s3_utils

import (
	"encoding/hex"
	"errors"
	"strings"
)

func ValidateInputs(s3URL string, sha256 string) error {
	if !strings.HasPrefix(s3URL, "s3://") {
		return errors.New("invalid S3 URL format. URL should be of the format 's3://bucket/path/to/file'")
	}

	// Normalize case and validate SHA256
	sha256 = strings.ToLower(sha256)
	if len(sha256) != 64 {
		return errors.New("invalid SHA256 hash. Ensure it is a valid 64-character hexadecimal string.")
	}

	if _, err := hex.DecodeString(sha256); err != nil {
		return errors.New("invalid SHA256 hash. Ensure it is a valid 64-character hexadecimal string.")
	}

	return nil
}
