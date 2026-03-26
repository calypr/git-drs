package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/lfs"
	"gocloud.dev/blob"
)

// Download extracts the first non-empty access URL from a
// DRSObject, performs a HEAD preflight for that URL, downloads object bytes to
// a temporary file, and returns the computed SHA256 and temporary file path.
func Download(ctx context.Context, drsObj *drs.DRSObject) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if drsObj == nil {
		return "", "", fmt.Errorf("drs object is nil")
	}

	rawURL, err := firstAccessURL(drsObj)
	if err != nil {
		return "", "", err
	}

	headMeta, err := HeadObject(ctx, rawURL)
	if err != nil {
		return "", "", fmt.Errorf("head preflight failed: %w", err)
	}

	rc, err := openObjectReader(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()

	etag := headMeta.ETag
	subdir1, subdir2 := "xx", "yy"
	if len(etag) >= 4 {
		subdir1 = etag[0:2]
		subdir2 = etag[2:4]
	}
	objName := etag
	if objName == "" {
		objName = "unknown-etag"
	}
	tmpDir := filepath.Join(os.TempDir(), "git-drs", "tmp-objects", subdir1, subdir2)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", tmpDir, err)
	}
	tmpPath := filepath.Join(tmpDir, objName)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", tmpPath, err)
	}

	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, h), rc); err != nil {
		return "", "", fmt.Errorf("download object: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return "", "", fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", "", fmt.Errorf("close temp file: %w", err)
	}

	computedSHA := fmt.Sprintf("%x", h.Sum(nil))

	_, lfsRoot, err := lfs.GetGitRootDirectories(ctx)
	if err != nil {
		return "", "", fmt.Errorf("get git root directories: %w", err)
	}

	oid := computedSHA // sha of sentinel drsObj.Checksums.SHA256
	dstDir := filepath.Join(lfsRoot, "objects", oid[0:2], oid[2:4])
	dstPath := filepath.Join(dstDir, oid)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dstDir, err)
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		return "", "", fmt.Errorf("rename %s to %s: %w", tmpPath, dstPath, err)
	}
	cleanup = false
	return computedSHA, dstPath, nil

}

func firstAccessURL(drsObj *drs.DRSObject) (string, error) {
	if len(drsObj.AccessMethods) == 0 {
		return "", fmt.Errorf("drs object has no access methods")
	}
	for _, am := range drsObj.AccessMethods {
		u := strings.TrimSpace(am.AccessURL.URL)
		if u != "" {
			return u, nil
		}
	}
	return "", fmt.Errorf("drs object has no access URL")
}

func openObjectReader(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	platform := DetectPlatform(rawURL)
	if platform == PlatformUnknown {
		return nil, fmt.Errorf("unsupported URL: cannot detect cloud platform for %q", rawURL)
	}

	bucketURL, _, key, err := toCDKBucketURL(rawURL, platform)
	if err != nil {
		return nil, err
	}

	b, err := openBucket(ctx, bucketURL)
	if err != nil {
		return nil, fmt.Errorf("%s: open bucket: %w", platform, err)
	}

	r, err := b.NewReader(ctx, key, nil)
	if err != nil {
		_ = b.Close()
		return nil, fmt.Errorf("%s: new reader %s: %w", platform, key, err)
	}

	return &bucketReader{Reader: r, bucket: b}, nil
}

type bucketReader struct {
	*blob.Reader
	bucket *blob.Bucket
}

func (br *bucketReader) Close() error {
	err := br.Reader.Close()
	_ = br.bucket.Close()
	return err
}

// DownloadOLD deprecated: downloads the S3 object to a temporary file while computing its SHA256 hash.
// returns the computed SHA256 hash, temporary path and any error encountered.
func DownloadOLD(ctx context.Context, info *S3Object, s3Input S3ObjectParameters, lfsRoot string) (string, string, error) {
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

// GetSHA256 computes the SHA256 hash of the input string and returns it as a hex-encoded string.
func GetSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
