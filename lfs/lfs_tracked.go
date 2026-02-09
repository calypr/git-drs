package lfs

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
)

// IsLFSTracked returns true if the given path is tracked by Git LFS
// (i.e. has `filter=lfs` via git attributes).
func IsLFSTracked(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	// Git prefers forward slashes, even on macOS/Linux
	cleanPath := filepath.ToSlash(path)

	cmd := exec.Command(
		"git",
		"check-attr",
		"filter",
		"--",
		cleanPath,
	)

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git check-attr failed: %w (%s)", err, out.String())
	}

	// Expected output:
	// path: filter: lfs
	// path: filter: unspecified
	//
	// Format is stable and documented, but some git wrappers print extra
	// debugging lines. eg GIT_TRACE=1 GIT_TRANSFER_TRACE=1
	// Only consider the line that starts with the queried
	// path so we do not parse unrelated output.
	output := out.String()
	prefix := cleanPath + ":"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, prefix) {
			continue
		}

		fields := strings.SplitN(line, ":", 3)
		if len(fields) < 3 {
			continue
		}

		value := strings.TrimSpace(fields[2])
		return value == "lfs", nil
	}

	return false, nil
}

// LfsFileInfo represents the information about an LFS file
type LfsFileInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Checkout   bool   `json:"checkout"`
	Downloaded bool   `json:"downloaded"`
	OidType    string `json:"oid_type"`
	Oid        string `json:"oid"`
	Version    string `json:"version"`
}

type lfsLsOutput struct {
	Files []LfsFileInfo `json:"files"`
}

// CheckIfLfsFile checks if a given file is tracked by Git LFS.
// Returns true and file info if it's an LFS file, false otherwise.
func CheckIfLfsFile(fileName string) (bool, *LfsFileInfo, error) {
	// Use git lfs ls-files -I to check if specific file is LFS tracked
	cmd := exec.Command("git", "lfs", "ls-files", "-I", fileName, "--json")
	out, err := cmd.Output()
	if err != nil {
		// If git lfs ls-files returns error, the file is not LFS tracked
		return false, nil, nil
	}

	// If output is empty, file is not LFS tracked
	if len(strings.TrimSpace(string(out))) == 0 {
		return false, nil, nil
	}

	// Parse the JSON output
	var output lfsLsOutput
	err = sonic.ConfigFastest.Unmarshal(out, &output)
	if err != nil {
		return false, nil, fmt.Errorf("error unmarshaling git lfs ls-files output for %s: %v", fileName, err)
	}

	// If no files in output, not LFS tracked
	if len(output.Files) == 0 {
		return false, nil, nil
	}

	return true, &output.Files[0], nil
}
