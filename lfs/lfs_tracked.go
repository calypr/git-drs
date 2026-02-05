package lfs

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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
	// Format is stable and documented.
	fields := strings.Split(out.String(), ":")
	if len(fields) < 3 {
		return false, nil
	}

	value := strings.TrimSpace(fields[2])
	return value == "lfs", nil
}
