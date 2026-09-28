package transfer

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

type resumableDownloadKey struct{}

func WithResumableDownload(ctx context.Context) context.Context {
	return context.WithValue(ctx, resumableDownloadKey{}, true)
}

func isResumableDownload(ctx context.Context) bool {
	return ctx.Value(resumableDownloadKey{}) == true
}

type downloadCheckpoint struct {
	Identity string `json:"identity"`
	Size     int64  `json:"size"`
	Complete bool   `json:"complete"`
}

// IncompleteDownloadCheckpoint reports whether syfon left a transfer in
// progress at path. A preallocated multipart file can have its final size
// without containing all of its bytes, so size alone cannot establish that it
// is a complete cached object.
func IncompleteDownloadCheckpoint(path string, expectedSize int64) bool {
	data, err := os.ReadFile(path + ".syfon-download.json")
	if err != nil {
		return false
	}
	var checkpoint downloadCheckpoint
	return json.Unmarshal(data, &checkpoint) == nil && !checkpoint.Complete && strings.TrimSpace(checkpoint.Identity) != "" && checkpoint.Size == expectedSize
}

func resumableOffset(path string, expectedSize int64) int64 {
	if !IncompleteDownloadCheckpoint(path, expectedSize) {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() >= expectedSize {
		return 0
	}
	return info.Size()
}
