package lfss3

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Download downloads the S3 object to a temporary file while computing its SHA256 hash.
// returns the computed SHA256 hash, temporary path and any error encountered.
func Download(ctx context.Context, info *S3Object, s3Input S3ObjectParameters, lfsRoot string) (string, string, error) {
	// 2) object destination
	etag := info.ETag
	subdir1, subdir2 := "xx", "yy"
	if len(etag) >= 4 {
		subdir1 = etag[0:2]
		subdir2 = etag[2:4]
	}
	objName := etag
	if objName == "" {
		objName = "unknown-etag"
	}
	tmpDir := filepath.Join(lfsRoot, "tmp-objects", subdir1, subdir2)
	tmpObj := filepath.Join(tmpDir, objName)

	// 3) fetch bytes -> tmp, compute sha+count

	// Create the temporary directory and file where the S3 object will be streamed while computing its hash and size.
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", tmpDir, err)
	}

	f, err := os.Create(tmpObj)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", tmpObj, err)
	}
	// ensure any leftover file is closed and error propagated via named return
	defer func() {
		if f != nil {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("close tmp file: %w", cerr)
			}
		}
	}()

	h := sha256.New()

	var reader io.ReadCloser
	reader, err = AgentFetchReader(ctx, s3Input)
	if err != nil {
		return "", "", fmt.Errorf("fetch reader: %w", err)
	}
	// ensure close on any early return; propagate close error via named return
	defer func() {
		if reader != nil {
			if cerr := reader.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("close reader: %w", cerr)
			}
		}
	}()

	n, err := io.Copy(io.MultiWriter(f, h), reader)
	if err != nil {
		return "", "", fmt.Errorf("copy bytes to %s: %w", tmpObj, err)
	}

	// explicitly close reader and handle error
	if cerr := reader.Close(); cerr != nil {
		return "", "", fmt.Errorf("close reader: %w", cerr)
	}
	reader = nil

	// ensure data is flushed to disk
	if err = f.Sync(); err != nil {
		return "", "", fmt.Errorf("sync %s: %w", tmpObj, err)
	}

	// explicitly close tmp file before rename
	if cerr := f.Close(); cerr != nil {
		return "", "", fmt.Errorf("close %s: %w", tmpObj, cerr)
	}
	f = nil

	// use n (bytes written) to avoid unused var warnings
	_ = n

	// compute hex SHA256 of the fetched content
	computedSHA := fmt.Sprintf("%x", h.Sum(nil))
	return computedSHA, tmpObj, nil
}
