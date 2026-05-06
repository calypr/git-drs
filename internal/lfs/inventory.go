package lfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/hash"
)

// LfsFileInfo represents a Git LFS pointer discovered in Git history or CLI output.
type LfsFileInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Checkout   bool   `json:"checkout"`
	Downloaded bool   `json:"downloaded"`
	IsPointer  bool   `json:"is_pointer,omitempty"`
	OidType    string `json:"oid_type"`
	Oid        string `json:"oid"`
	Version    string `json:"version"`
}

func IsLFSTracked(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	cmd := exec.Command("git", "check-attr", "filter", "--", filepath.ToSlash(path))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("git check-attr failed: %w (%s)", err, out.String())
	}

	fields := strings.Split(out.String(), ":")
	if len(fields) < 3 {
		return false, nil
	}
	return strings.TrimSpace(fields[2]) == "lfs", nil
}

func GetAllLfsFiles(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]LfsFileInfo, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if gitRemoteName == "" {
		gitRemoteName = "origin"
	}
	if gitRemoteLocation != "" {
		logger.Debug(fmt.Sprintf("Using git remote %s at %s for LFS inventory", gitRemoteName, gitRemoteLocation))
	} else {
		logger.Debug(fmt.Sprintf("Using git remote %s for LFS inventory", gitRemoteName))
	}
	logger.Debug("Scanning Git refs for LFS pointer files (no git lfs CLI required)")

	// no timeout for now
	ctx := context.Background()
	refs := buildRefs(branches)
	lfsFileMap := make(map[string]LfsFileInfo)
	for _, ref := range refs {
		if err := addFilesFromRef(ctx, repoDir, ref, logger, lfsFileMap); err != nil {
			return nil, err
		}
	}

	return lfsFileMap, nil
}

// GetWorktreeLfsFiles scans the current checkout and returns tracked files whose
// worktree content is currently a valid Git LFS pointer. This is the fast path
// for interactive commands like `git-drs ls-files`.
func GetWorktreeLfsFiles(logger *slog.Logger) (map[string]LfsFileInfo, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	logger.Debug("Scanning current worktree for LFS pointer files")
	ctx := context.Background()
	paths, err := listTrackedWorktreeFiles(ctx, repoDir)
	if err != nil {
		return nil, err
	}
	files := make(map[string]LfsFileInfo)
	for _, path := range paths {
		payload, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(path)))
		if err != nil {
			continue
		}
		pointer, ok := parseLFSPointer(string(payload))
		if !ok {
			continue
		}
		files[path] = LfsFileInfo{
			Name:      path,
			Size:      pointer.Size,
			IsPointer: true,
			OidType:   pointer.OidType,
			Oid:       pointer.Oid,
			Version:   pointer.Version,
		}
	}
	return files, nil
}

func addFilesFromRef(ctx context.Context, repoDir, ref string, logger *slog.Logger, lfsFileMap map[string]LfsFileInfo) error {
	paths, err := grepPointerPaths(ctx, repoDir, ref)
	if err != nil {
		return fmt.Errorf("git grep failed for %s: %w", ref, err)
	}
	for _, path := range paths {
		blob, err := runGitCommand(ctx, repoDir, "show", fmt.Sprintf("%s:%s", ref, path))
		if err != nil {
			logger.Debug(fmt.Sprintf("skipping path %s in %s: unable to read blob", path, ref))
			continue
		}

		pointer, ok := parseLFSPointer(blob)
		if !ok {
			continue
		}

		lfsFileMap[path] = LfsFileInfo{
			Name:      path,
			Size:      pointer.Size,
			IsPointer: true,
			OidType:   pointer.OidType,
			Oid:       pointer.Oid,
			Version:   pointer.Version,
		}
	}

	return nil
}

func listTrackedWorktreeFiles(ctx context.Context, repoDir string) ([]string, error) {
	out, err := runGitCommand(ctx, repoDir, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("git ls-files failed: %w", err)
	}
	raw := strings.Split(out, "\x00")
	paths := make([]string, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		paths = append(paths, entry)
	}
	return paths, nil
}

func grepPointerPaths(ctx context.Context, repoDir, ref string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "grep", "-z", "-l", "https://git-lfs.github.com/spec/v1", ref, "--")
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}

	raw := strings.Split(stdout.String(), "\x00")
	paths := make([]string, 0, len(raw))
	prefix := ref + ":"
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		path := entry
		if strings.HasPrefix(path, prefix) {
			path = strings.TrimPrefix(path, prefix)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func runGitCommand(ctx context.Context, repoDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

type lfsPointer struct {
	Version string
	OidType string
	Oid     string
	Size    int64
}

func parseLFSPointer(content string) (lfsPointer, bool) {
	var p lfsPointer
	sha256Re := regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "version ") {
			p.Version = strings.TrimSpace(strings.TrimPrefix(line, "version "))
			continue
		}
		if strings.HasPrefix(line, "oid ") {
			oidSpec := strings.TrimSpace(strings.TrimPrefix(line, "oid "))
			parts := strings.SplitN(oidSpec, ":", 2)
			if len(parts) != 2 {
				return lfsPointer{}, false
			}
			p.OidType = strings.TrimSpace(parts[0])
			p.Oid = strings.TrimSpace(parts[1])
			continue
		}
		if strings.HasPrefix(line, "size ") {
			szStr := strings.TrimSpace(strings.TrimPrefix(line, "size "))
			sz, err := strconv.ParseInt(szStr, 10, 64)
			if err != nil || sz < 0 {
				return lfsPointer{}, false
			}
			p.Size = sz
		}
	}

	if p.Version == "" || p.OidType == "" || p.Oid == "" {
		return lfsPointer{}, false
	}
	if p.OidType != "sha256" || !sha256Re.MatchString(p.Oid) {
		return lfsPointer{}, false
	}

	return p, true
}

// ParseLFSPointer extracts the sha256 object ID and size from an LFS pointer
// payload. It returns ok=false when data is not a valid Git LFS pointer.
func ParseLFSPointer(data []byte) (oid string, size int64, ok bool) {
	pointer, ok := parseLFSPointer(string(data))
	if !ok {
		return "", 0, false
	}
	return pointer.Oid, pointer.Size, true
}

func buildRefs(branches []string) []string {
	if len(branches) == 0 {
		return []string{"HEAD"}
	}
	refs := make([]string, 0, len(branches))
	seen := make(map[string]struct{})
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		ref := branch
		if branch != "HEAD" && !strings.HasPrefix(branch, "refs/") {
			ref = fmt.Sprintf("refs/heads/%s", branch)
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return []string{"HEAD"}
	}
	return refs
}

// CreateLfsPointer creates a Git LFS pointer file for the given DRS object.
func CreateLfsPointer(drsObj *drsapi.DrsObject, dst string) error {
	hashInfo := hash.ConvertDrsChecksumsToHashInfo(drsObj.Checksums)
	shaSum := hashInfo.SHA256
	if shaSum == "" {
		return fmt.Errorf("no sha256 checksum found for DRS object")
	}

	// create pointer file content
	pointerContent := "version https://git-lfs.github.com/spec/v1\n"
	pointerContent += fmt.Sprintf("oid sha256:%s\n", shaSum)
	pointerContent += fmt.Sprintf("size %d\n", drsObj.Size)

	// write to file
	err := os.WriteFile(dst, []byte(pointerContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write LFS pointer file: %w", err)
	}

	return nil
}
