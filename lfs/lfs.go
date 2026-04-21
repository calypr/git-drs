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

	"github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/pkg/hash"
)

type DryRunSpec struct {
	Remote string // e.g. "origin"
	Ref    string // e.g. "refs/heads/main" or "HEAD"
}

// RunPushDryRun executes: git lfs push --dry-run <remote> <ref>
func RunPushDryRun(ctx context.Context, repoDir string, spec DryRunSpec, logger *slog.Logger) (string, error) {
	if spec.Remote == "" || spec.Ref == "" {
		return "", errors.New("missing remote or ref")
	}

	// Debug-print the command to stderr
	fullCmd := []string{"git", "lfs", "push", "--dry-run", spec.Remote, spec.Ref}
	logger.Debug(fmt.Sprintf("running command: %v", fullCmd))

	cmd := exec.CommandContext(ctx, "git", "lfs", "push", "--dry-run", spec.Remote, spec.Ref)
	cmd.Dir = repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("git lfs push --dry-run failed: %s", msg)
	}
	return out, nil
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

func addFilesFromRef(ctx context.Context, repoDir, ref string, logger *slog.Logger, lfsFileMap map[string]LfsFileInfo) error {
	out, err := runGitCommand(ctx, repoDir, "ls-tree", "-r", "-z", "--long", ref)
	if err != nil {
		return fmt.Errorf("git ls-tree failed for %s: %w", ref, err)
	}

	entries := strings.Split(out, "\x00")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		oid, path, err := parseLsTreeEntry(entry)
		if err != nil {
			logger.Debug(fmt.Sprintf("skipping unparseable ls-tree entry for %s: %q", ref, entry))
			continue
		}

		blob, err := runGitCommand(ctx, repoDir, "cat-file", "-p", oid)
		if err != nil {
			logger.Debug(fmt.Sprintf("skipping path %s in %s: unable to read blob %s", path, ref, oid))
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

func parseLsTreeEntry(entry string) (string, string, error) {
	tab := strings.Index(entry, "\t")
	if tab < 0 {
		return "", "", fmt.Errorf("missing tab separator")
	}

	meta := strings.Fields(entry[:tab])
	if len(meta) < 3 {
		return "", "", fmt.Errorf("invalid ls-tree metadata")
	}
	if meta[1] != "blob" {
		return "", "", fmt.Errorf("not a blob entry")
	}

	oid := strings.TrimSpace(meta[2])
	path := strings.TrimSpace(entry[tab+1:])
	if oid == "" || path == "" {
		return "", "", fmt.Errorf("missing oid or path")
	}
	return oid, path, nil
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

func addFilesFromDryRun(out, repoDir string, logger *slog.Logger, lfsFileMap map[string]LfsFileInfo) error {
	// Log when dry-run returns no output to help with debugging
	if strings.TrimSpace(out) == "" {
		logger.Debug("No LFS files to push (dry-run returned no output)")
		return nil
	}

	// accept lowercase or uppercase hex
	sha256Re := regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		oid := ""
		oidIndex := -1
		for i, p := range parts {
			if sha256Re.MatchString(p) {
				oid = p
				oidIndex = i
				break
			}
		}
		if oid == "" {
			logger.Debug(fmt.Sprintf("skipping LFS line with no oid: %q", line))
			continue
		}

		// Preserve full path text (including spaces) by slicing from the first oid occurrence.
		path := ""
		if idx := strings.Index(line, oid); idx >= 0 {
			path = strings.TrimSpace(line[idx+len(oid):])
		}
		if path == "" && oidIndex+1 < len(parts) {
			path = strings.Join(parts[oidIndex+1:], " ")
		}
		path = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(path, "=>"), "->"))
		path = strings.TrimSpace(strings.TrimPrefix(path, "=>"))
		path = strings.TrimSpace(strings.TrimPrefix(path, "->"))
		if path == "" {
			logger.Debug(fmt.Sprintf("skipping LFS line with empty path: %q", line))
			continue
		}

		// Validate OID looks like a SHA256 hex string.
		// (kept as a guard in case extraction logic changes)
		if !sha256Re.MatchString(oid) {
			logger.Debug(fmt.Sprintf("skipping LFS line with invalid oid %q: %q", oid, line))
			continue
		}

		// see https://github.com/calypr/git-drs/issues/124#issuecomment-3721837089
		if oid == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" && strings.Contains(path, ".gitattributes") {
			logger.Debug(fmt.Sprintf("skipping empty LFS pointer for %s", path))
			continue
		}
		// Remove a trailing parenthetical suffix from p, e.g.:
		// "path/to/file.dat (100 KB)" -> "path/to/file.dat"
		if idx := strings.LastIndex(path, " ("); idx != -1 && strings.HasSuffix(path, ")") {
			path = strings.TrimSpace(path[:idx])
		}
		size := int64(0)
		absPath := path
		if repoDir != "" && !filepath.IsAbs(path) {
			absPath = filepath.Join(repoDir, path)
		}
		if stat, err := os.Stat(absPath); err == nil {
			size = stat.Size()
		} else {
			logger.Error(fmt.Sprintf("could not stat file %s: %v", path, err))
			continue
		}

		isPointer := false
		// If the file is small, read it and detect LFS pointer signature.
		// Pointer files are textual and include the LFS spec version + an oid line.
		if size > 0 && size < 2048 {
			if data, readErr := os.ReadFile(absPath); readErr == nil {
				s := strings.TrimSpace(string(data))
				if strings.Contains(s, "version https://git-lfs.github.com/spec/v1") && strings.Contains(s, "oid sha256:") {
					isPointer = true
				}
			}
		}

		lfsFileMap[path] = LfsFileInfo{
			Name:      path,
			Size:      size,
			IsPointer: isPointer,
			OidType:   "sha256",
			Oid:       oid,
			Version:   "https://git-lfs.github.com/spec/v1",
		}
	}

	return nil
}

// CreateLfsPointer creates a Git LFS pointer file for the given DRS object.
func CreateLfsPointer(drsObj *drs.DRSObject, dst string) error {
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
