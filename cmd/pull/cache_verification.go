package pull

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/syfon/client/hash"
)

const cacheVerificationVersion = 1

type cacheVerification struct {
	Version    int    `json:"version"`
	OID        string `json:"oid"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
	ModifiedNS int64  `json:"modified_ns"`
	Device     uint64 `json:"device"`
	Inode      uint64 `json:"inode"`
}

func expectedPointerSHA256(file pointerFile) string {
	if file.SHA256 != "" {
		return strings.ToLower(strings.TrimSpace(file.SHA256))
	}
	if file.Placeholder || strings.HasPrefix(file.Oid, "//") || strings.HasPrefix(strings.ToLower(file.Oid), "drs://") {
		return ""
	}
	return strings.ToLower(hash.NormalizeChecksum(file.Oid))
}

func cacheVerificationPath(path string) string { return path + ".git-drs-verified.json" }

func verifiedCacheInfo(path string, file pointerFile) (os.FileInfo, bool) {
	if expectedPointerSHA256(file) == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() != file.Size {
		return nil, false
	}
	data, err := os.ReadFile(cacheVerificationPath(path))
	if err != nil {
		return nil, false
	}
	var record cacheVerification
	if json.Unmarshal(data, &record) != nil || record != verificationRecord(file, info) {
		return nil, false
	}
	return info, true
}

func verificationRecord(file pointerFile, info os.FileInfo) cacheVerification {
	device, inode := cacheFileIdentity(info)
	return cacheVerification{
		Version:    cacheVerificationVersion,
		OID:        file.Oid,
		SHA256:     expectedPointerSHA256(file),
		Size:       info.Size(),
		ModifiedNS: info.ModTime().UnixNano(),
		Device:     device,
		Inode:      inode,
	}
}

func rememberVerifiedCache(path string, file pointerFile, state cachedObjectState) {
	if !state.complete || state.info == nil || expectedPointerSHA256(file) == "" {
		return
	}
	changed, err := cachedObjectChanged(path, state)
	if err != nil || changed {
		return
	}
	data, err := json.Marshal(verificationRecord(file, state.info))
	if err != nil {
		return
	}
	marker := cacheVerificationPath(path)
	tmp, err := os.CreateTemp(filepath.Dir(marker), ".git-drs-verified-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmp.Name(), marker)
}

func rememberVerifiedPath(path string, file pointerFile) {
	info, err := os.Stat(path)
	if err == nil {
		rememberVerifiedCache(path, file, cachedObjectState{exists: true, complete: true, info: info})
	}
}
