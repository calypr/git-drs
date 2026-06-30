package lfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	return isTrackedFilter(strings.TrimSpace(fields[2])), nil
}

// GetLfsFilesForRefs scans arbitrary refs or SHAs and returns the LFS pointer
// files present in those trees.
func GetLfsFilesForRefs(refs []string, logger *slog.Logger) (map[string]LfsFileInfo, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	lfsFileMap := make(map[string]LfsFileInfo)
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		if err := addFilesFromRef(ctx, repoDir, ref, logger, lfsFileMap); err != nil {
			return nil, err
		}
	}
	return lfsFileMap, nil
}

// GetReachablePointerFilesForRef scans a Git ref/tree and returns valid Git
// LFS/DRS pointer blobs reachable from that tree.
func GetReachablePointerFilesForRef(ref string, logger *slog.Logger) (map[string]LfsFileInfo, error) {
	return GetLfsFilesForRefs([]string{ref}, logger)
}

// GetLfsFilesForRefPaths scans the given paths in a specific ref/tree and
// returns only those entries whose blob content is a valid Git LFS pointer.
func GetLfsFilesForRefPaths(ref string, paths []string, logger *slog.Logger) (map[string]LfsFileInfo, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "HEAD"
	}
	normalized := uniquePaths(paths)
	files := make(map[string]LfsFileInfo)
	if len(normalized) == 0 {
		return files, nil
	}
	if err := addFilesFromPaths(context.Background(), repoDir, ref, normalized, logger, files); err != nil {
		return nil, err
	}
	return files, nil
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

// GetTrackedLfsFiles scans the current checkout and returns files that are LFS
// tracked according to Git attributes. Pointer metadata is taken from the
// worktree when still present, or from the index when the worktree has already
// been hydrated.
func GetTrackedLfsFiles(logger *slog.Logger) (map[string]LfsFileInfo, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	repoDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	logger.Debug("Scanning current worktree for LFS-tracked files")
	ctx := context.Background()
	paths, err := listTrackedWorktreeFiles(ctx, repoDir)
	if err != nil {
		return nil, err
	}
	tracked, err := filterLfsTrackedPaths(ctx, repoDir, paths)
	if err != nil {
		return nil, err
	}
	files := make(map[string]LfsFileInfo, len(tracked))
	for _, path := range tracked {
		if info, ok := readWorktreePointerInfo(repoDir, path); ok {
			files[path] = info
			continue
		}
		if info, ok := readIndexPointerInfo(ctx, repoDir, path); ok {
			files[path] = info
		}
	}
	return files, nil
}

func addFilesFromRef(ctx context.Context, repoDir, ref string, logger *slog.Logger, lfsFileMap map[string]LfsFileInfo) error {
	paths, err := grepPointerPaths(ctx, repoDir, ref)
	if err != nil {
		return fmt.Errorf("git grep failed for %s: %w", ref, err)
	}
	return addFilesFromPaths(ctx, repoDir, ref, paths, logger, lfsFileMap)
}

func addFilesFromPaths(ctx context.Context, repoDir, ref string, paths []string, _ *slog.Logger, lfsFileMap map[string]LfsFileInfo) error {
	if len(paths) == 0 {
		return nil
	}

	blobs, err := readRefBlobsBatch(ctx, repoDir, ref, paths)
	if err != nil {
		return fmt.Errorf("git cat-file batch failed for %s: %w", ref, err)
	}
	for path, blob := range blobs {
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

func uniquePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func readRefBlobsBatch(ctx context.Context, repoDir, ref string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil
	}

	cmd := exec.CommandContext(ctx, "git", "cat-file", "--batch")
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(joinBatchSpecs(ref, paths))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}

	reader := bytes.NewReader(stdout.Bytes())
	blobs := make(map[string]string, len(paths))
	for _, path := range paths {
		header, err := readBatchHeader(reader)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(header, " missing") {
			continue
		}
		size, err := parseBatchHeaderSize(header)
		if err != nil {
			return nil, err
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, fmt.Errorf("read batch payload for %s:%s: %w", ref, path, err)
		}
		if err := consumeBatchSeparator(reader); err != nil {
			return nil, err
		}
		blobs[path] = string(payload)
	}
	return blobs, nil
}

func joinBatchSpecs(ref string, paths []string) string {
	var b strings.Builder
	for _, path := range paths {
		b.WriteString(ref)
		b.WriteByte(':')
		b.WriteString(path)
		b.WriteByte('\n')
	}
	return b.String()
}

func readBatchHeader(r *bytes.Reader) (string, error) {
	var line []byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if b == '\n' {
			return strings.TrimSpace(string(line)), nil
		}
		line = append(line, b)
	}
}

func parseBatchHeaderSize(header string) (int, error) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, fmt.Errorf("unexpected git cat-file batch header %q", header)
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 {
		return 0, fmt.Errorf("unexpected git cat-file object size in header %q", header)
	}
	return size, nil
}

func consumeBatchSeparator(r *bytes.Reader) error {
	b, err := r.ReadByte()
	if err != nil {
		return err
	}
	if b != '\n' {
		return fmt.Errorf("unexpected git cat-file batch separator %q", string([]byte{b}))
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

func filterLfsTrackedPaths(ctx context.Context, repoDir string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	cmd := exec.CommandContext(ctx, "git", "check-attr", "-z", "--stdin", "filter")
	cmd.Dir = repoDir
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git check-attr failed: %s", msg)
	}

	raw := strings.Split(stdout.String(), "\x00")
	filtered := make([]string, 0, len(paths))
	for i := 0; i+2 < len(raw); i += 3 {
		path := strings.TrimSpace(raw[i])
		attr := strings.TrimSpace(raw[i+1])
		value := strings.TrimSpace(raw[i+2])
		if path == "" || attr != "filter" {
			continue
		}
		if isTrackedFilter(value) {
			filtered = append(filtered, path)
		}
	}
	return filtered, nil
}

func isTrackedFilter(value string) bool {
	switch strings.TrimSpace(value) {
	case "lfs", "drs":
		return true
	default:
		return false
	}
}

func readWorktreePointerInfo(repoDir, path string) (LfsFileInfo, bool) {
	payload, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(path)))
	if err != nil {
		return LfsFileInfo{}, false
	}
	pointer, ok := parseLFSPointer(string(payload))
	if !ok {
		return LfsFileInfo{}, false
	}
	return LfsFileInfo{
		Name:      path,
		Size:      pointer.Size,
		IsPointer: true,
		OidType:   pointer.OidType,
		Oid:       pointer.Oid,
		Version:   pointer.Version,
	}, true
}

func readIndexPointerInfo(ctx context.Context, repoDir, path string) (LfsFileInfo, bool) {
	blob, err := runGitCommand(ctx, repoDir, "show", ":"+path)
	if err != nil {
		return LfsFileInfo{}, false
	}
	pointer, ok := parseLFSPointer(blob)
	if !ok {
		return LfsFileInfo{}, false
	}
	return LfsFileInfo{
		Name:      path,
		Size:      pointer.Size,
		IsPointer: false,
		OidType:   pointer.OidType,
		Oid:       pointer.Oid,
		Version:   pointer.Version,
	}, true
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
