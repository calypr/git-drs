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
	"strings"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
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

	// no timeout for now
	ctx := context.Background()

	if gitRemoteName == "" {
		gitRemoteName = "origin"
	}
	if gitRemoteLocation != "" {
		logger.Debug(fmt.Sprintf("Using git remote %s at %s for LFS dry-run", gitRemoteName, gitRemoteLocation))
	} else {
		logger.Debug(fmt.Sprintf("Using git remote %s for LFS dry-run", gitRemoteName))
	}

	refs := buildRefs(branches)
	lfsFileMap := make(map[string]LfsFileInfo)
	for _, ref := range refs {
		spec := DryRunSpec{
			Remote: gitRemoteName,
			Ref:    ref,
		}
		out, err := RunPushDryRun(ctx, repoDir, spec, logger)
		if err != nil {
			return nil, err
		}

		if err := addFilesFromDryRun(out, repoDir, logger, lfsFileMap); err != nil {
			return nil, err
		}
	}

	return lfsFileMap, nil
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
		oid := parts[1]
		path := parts[len(parts)-1]

		// Validate OID looks like a SHA256 hex string.
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

		// If the file is small, read it and detect LFS pointer signature.
		// Pointer files are textual and include the LFS spec version + an oid line.
		//if size > 0 && size < 2048 {
		//	if data, readErr := os.ReadFile(absPath); readErr == nil {
		//		s := strings.TrimSpace(string(data))
		//		if strings.Contains(s, "version https://git-lfs.github.com/spec/v1") && strings.Contains(s, "oid sha256:") {
		//			logger.Warn(fmt.Sprintf("WARNING: Detected upload of lfs pointer file %s skipping", path))
		//			continue
		//		}
		//	}
		//}

		lfsFileMap[path] = LfsFileInfo{
			Name:    path,
			Size:    size,
			OidType: "sha256",
			Oid:     oid,
			Version: "https://git-lfs.github.com/spec/v1",
		}
	}

	return nil
}

// CreateLfsPointer creates a Git LFS pointer file for the given DRS object.
func CreateLfsPointer(drsObj *drs.DRSObject, dst string) error {
	sumMap := hash.ConvertHashInfoToMap(drsObj.Checksums)
	if len(sumMap) == 0 {
		return fmt.Errorf("no checksums found for DRS object")
	}

	// find sha256 checksum
	var shaSum string
	for csType, cs := range sumMap {
		if csType == hash.ChecksumTypeSHA256.String() {
			shaSum = cs
			break
		}
	}
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
