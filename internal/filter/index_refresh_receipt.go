package filter

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	IndexRefreshReceiptsEnv    = "GIT_DRS_INDEX_REFRESH_RECEIPTS"
	indexRefreshReceiptVersion = 1
)

// IndexRefreshReceipt records the hash computed while pull checked out a file.
// The filesystem identity prevents a later replacement or edit from reusing it.
type IndexRefreshReceipt struct {
	Path     string `json:"path"`
	OID      string `json:"oid"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified_ns"`
	Changed  int64  `json:"changed_ns"`
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
}

type indexRefreshReceiptManifest struct {
	Version  int                   `json:"version"`
	Receipts []IndexRefreshReceipt `json:"receipts"`
}

func NewIndexRefreshReceipt(path, oid, sha256 string, size int64, info os.FileInfo) (IndexRefreshReceipt, bool) {
	path = filepath.ToSlash(filepath.Clean(path))
	checksum, err := hex.DecodeString(sha256)
	if path == "." || path == ".." || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || oid == "" || err != nil || len(checksum) != 32 || size < 0 || info == nil || !info.Mode().IsRegular() || info.Size() != size {
		return IndexRefreshReceipt{}, false
	}
	device, inode, changed, ok := fileIdentity(info)
	if !ok {
		return IndexRefreshReceipt{}, false
	}
	return IndexRefreshReceipt{
		Path:     path,
		OID:      oid,
		SHA256:   strings.ToLower(sha256),
		Size:     size,
		Modified: info.ModTime().UnixNano(),
		Changed:  changed,
		Device:   device,
		Inode:    inode,
	}, true
}

func (r IndexRefreshReceipt) Matches(path string, info os.FileInfo) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if path != r.Path || info == nil || !info.Mode().IsRegular() || info.Size() != r.Size || info.ModTime().UnixNano() != r.Modified {
		return false
	}
	device, inode, changed, ok := fileIdentity(info)
	return ok && device == r.Device && inode == r.Inode && changed == r.Changed
}

// SameIndexRefreshFile reports whether a rename left the same file contents
// identity in place. Rename may update ctime, so the receipt captures ctime
// from the destination after this check succeeds.
func SameIndexRefreshFile(before, after os.FileInfo) bool {
	if before == nil || after == nil || !before.Mode().IsRegular() || !after.Mode().IsRegular() || before.Size() != after.Size() || before.ModTime().UnixNano() != after.ModTime().UnixNano() {
		return false
	}
	beforeDevice, beforeInode, _, beforeOK := fileIdentity(before)
	afterDevice, afterInode, _, afterOK := fileIdentity(after)
	return beforeOK && afterOK && beforeDevice == afterDevice && beforeInode == afterInode
}

func MarshalIndexRefreshReceipts(receipts []IndexRefreshReceipt) ([]byte, error) {
	return json.Marshal(indexRefreshReceiptManifest{Version: indexRefreshReceiptVersion, Receipts: receipts})
}

func readIndexRefreshReceipts() (map[string]IndexRefreshReceipt, error) {
	manifestPath := os.Getenv(IndexRefreshReceiptsEnv)
	if manifestPath == "" {
		return nil, nil
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest indexRefreshReceiptManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Version != indexRefreshReceiptVersion {
		return nil, nil
	}
	receipts := make(map[string]IndexRefreshReceipt, len(manifest.Receipts))
	duplicates := make(map[string]struct{})
	for _, receipt := range manifest.Receipts {
		path := filepath.ToSlash(filepath.Clean(receipt.Path))
		sha256, hashErr := hex.DecodeString(receipt.SHA256)
		if receipt.Path == "" || path != receipt.Path || path == "." || path == ".." || filepath.IsAbs(path) || strings.HasPrefix(path, "../") || receipt.OID == "" || hashErr != nil || len(sha256) != 32 || receipt.Size < 0 {
			continue
		}
		if _, exists := receipts[path]; exists {
			delete(receipts, path)
			duplicates[path] = struct{}{}
			continue
		}
		if _, duplicate := duplicates[path]; duplicate {
			continue
		}
		receipts[path] = receipt
	}
	return receipts, nil
}
