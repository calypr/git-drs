package lfs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/calypr/git-drs/gitrepo"
)

// runGitAllowMissing treats "key not found" as empty output, not an error.
func runGitAllowMissing(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	// Use Output() to get stdout only.
	// GIT_TRACE et al go to stderr.
	b, err := cmd.Output()
	//if err != nil {
	//	// "git config --get missing.key" exits 1 with empty output.
	//	var exitErr *exec.ExitError
	//	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
	//		return "", nil
	//	}
	//}
	if err != nil {
		// "git config --get missing.key" exits 1 with empty output.
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", nil
		}
		return "", fmt.Errorf("%v: >%s<", err, s)
	}
	return string(b), nil
}

// resolveLFSRoot implements:
// - if `git config --get lfs.storage` is set: use it
//   - if relative: resolve relative to GitCommonDir (this is how git-lfs treats it in practice)
//
// - else: <GitCommonDir>/lfs
func resolveLFSRoot(ctx context.Context, gitCommonDir string) (string, error) {
	// NOTE: git config --get returns exit status 1 if key not found.
	out, err := runGitAllowMissing(ctx, "config", "--get", "lfs.storage")
	if err != nil {
		return "", fmt.Errorf("git config --get lfs.storage failed: %w", err)
	}
	val := strings.TrimSpace(out)

	if val == "" {
		return filepath.Clean(filepath.Join(gitCommonDir, "lfs")), nil
	}

	// Expand ~ if present (nice-to-have).
	if strings.HasPrefix(val, "~") && (len(val) == 1 || val[1] == '/' || val[1] == '\\') {
		home, herr := userHomeDir()
		if herr == nil && home != "" {
			val = filepath.Join(home, strings.TrimPrefix(val, "~"))
		}
	}

	if !filepath.IsAbs(val) {
		val = filepath.Join(gitCommonDir, val)
	}
	return filepath.Clean(val), nil
}

func runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

func userHomeDir() (string, error) {
	// Avoid os/user on some cross-compile scenarios; keep it simple.
	if runtime.GOOS == "windows" {
		// Not your target, but safe fallback.
		return "", errors.New("home expansion not supported on windows in this helper")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home, nil
	}
	// macOS/Linux
	out, err := exec.Command("sh", "-lc", "printf %s \"$HOME\"").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func GetGitAttribute(ctx context.Context, attr string, path string) (string, error) {
	out, err := runGit(ctx, "check-attr", attr, "--", path)
	if err != nil {
		return "", fmt.Errorf("git check-attr failed: %w", err)
	}
	return out, nil
}

//
// --- Git helpers ---
//

func GitLFSTrack(ctx context.Context, path string) (bool, error) {
	out, err := GitLFSTrackPatterns(ctx, []string{path}, false, false)
	if err != nil {
		return false, fmt.Errorf("git drs track failed: %w", err)
	}
	return strings.Contains(out, "Tracking \""), nil
}

func GitLFSTrackPatterns(ctx context.Context, patterns []string, verbose bool, dryRun bool) (string, error) {
	_ = ctx
	changedAttribLines := make(map[string]string, len(patterns))
	var output strings.Builder

	attribContents, err := readLocalGitAttributes()
	if err != nil {
		return "", fmt.Errorf("git drs track failed: %w", err)
	}

	knownPatterns := parseKnownLFSPatterns(attribContents)

	for _, unsanitizedPattern := range patterns {
		pattern := trimCurrentPrefix(cleanRootPath(unsanitizedPattern))
		encodedArg := escapeAttrPattern(pattern)

		if knownLine, ok := knownPatterns[pattern]; ok {
			if strings.Contains(knownLine, "filter=drs") && strings.Contains(knownLine, "diff=drs") && strings.Contains(knownLine, "merge=drs") && strings.Contains(knownLine, "-text") {
				output.WriteString(fmt.Sprintf("%q already supported\n", pattern))
				continue
			}
		}

		changedAttribLines[pattern] = fmt.Sprintf("%s filter=drs diff=drs merge=drs -text", encodedArg)
		output.WriteString(fmt.Sprintf("Tracking %q\n", unescapeAttrPattern(encodedArg)))

		if verbose {
			output.WriteString(fmt.Sprintf("Searching for files matching pattern: %s\n", pattern))
			output.WriteString(fmt.Sprintf("Found %d files previously added to Git matching pattern: %s\n", 0, pattern))
		}
	}

	if !dryRun {
		if err := writeMergedGitAttributes(attribContents, changedAttribLines, false); err != nil {
			return "", fmt.Errorf("git drs track failed: %w", err)
		}
	}

	return output.String(), nil
}

func GitLFSListTrackedPatterns(ctx context.Context, verbose bool) (string, error) {
	_ = ctx
	_ = verbose

	attribContents, err := readLocalGitAttributes()
	if err != nil {
		return "", fmt.Errorf("git drs track failed: %w", err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(attribContents))
	var patterns []string
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "filter=drs") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		patterns = append(patterns, unescapeAttrPattern(fields[0]))
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("git drs track failed: parse .gitattributes: %w", err)
	}

	if len(patterns) == 0 {
		return "", nil
	}

	var out strings.Builder
	out.WriteString("Listing tracked patterns\n")
	for _, p := range patterns {
		out.WriteString(fmt.Sprintf("    %s (.gitattributes)\n", p))
	}
	return out.String(), nil
}

func GitLFSUntrackPatterns(ctx context.Context, patterns []string, verbose bool, dryRun bool) (string, error) {
	_ = ctx
	_ = verbose

	attribContents, err := readLocalGitAttributes()
	if err != nil {
		return "", fmt.Errorf("git drs untrack failed: %w", err)
	}
	if len(attribContents) == 0 {
		return "", nil
	}

	removeSet := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		escaped := escapeAttrPattern(trimCurrentPrefix(p))
		removeSet[escaped] = struct{}{}
	}

	var out strings.Builder
	var keptLines []string
	scanner := bufio.NewScanner(bytes.NewReader(attribContents))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "filter=drs") {
			keptLines = append(keptLines, line)
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 1 {
			keptLines = append(keptLines, line)
			continue
		}

		path := trimCurrentPrefix(fields[0])
		if _, ok := removeSet[path]; ok {
			out.WriteString(fmt.Sprintf("Untracking %q\n", unescapeAttrPattern(path)))
			continue
		}

		keptLines = append(keptLines, line)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("git lfs untrack failed: parse .gitattributes: %w", err)
	}

	if !dryRun {
		content := strings.Join(keptLines, "\n")
		if content != "" {
			content += "\n"
		}
		if err := os.WriteFile(".gitattributes", []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("git lfs untrack failed: write .gitattributes: %w", err)
		}
	}

	return out.String(), nil
}

func readLocalGitAttributes() ([]byte, error) {
	data, err := os.ReadFile(".gitattributes")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read .gitattributes: %w", err)
	}
	return data, nil
}

func parseKnownLFSPatterns(content []byte) map[string]string {
	known := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, "filter=drs") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		known[unescapeAttrPattern(fields[0])] = line
	}
	return known
}

func writeMergedGitAttributes(existing []byte, changed map[string]string, dryRun bool) error {
	if dryRun {
		return nil
	}

	var merged []string
	if len(existing) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(existing))
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				pat := unescapeAttrPattern(fields[0])
				if newline, ok := changed[pat]; ok {
					merged = append(merged, newline)
					delete(changed, pat)
					continue
				}
			}
			merged = append(merged, line)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("parse .gitattributes: %w", err)
		}
	}

	for _, newline := range changed {
		merged = append(merged, newline)
	}

	content := strings.Join(merged, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(".gitattributes", []byte(content), 0o644); err != nil {
		return fmt.Errorf("write .gitattributes: %w", err)
	}
	return nil
}

func cleanRootPath(pattern string) string {
	return strings.TrimPrefix(pattern, "/")
}

func trimCurrentPrefix(path string) string {
	return strings.TrimPrefix(path, "./")
}

var trackEscapePatterns = map[string]string{
	" ": "[[:space:]]",
	"#": "\\#",
}

func escapeAttrPattern(s string) string {
	var escaped string
	if runtime.GOOS == "windows" {
		escaped = strings.ReplaceAll(s, `\\`, "/")
	} else {
		escaped = strings.ReplaceAll(s, `\\`, `\\\\`)
	}

	for from, to := range trackEscapePatterns {
		escaped = strings.ReplaceAll(escaped, from, to)
	}

	return escaped
}

func unescapeAttrPattern(escaped string) string {
	unescaped := escaped

	for to, from := range trackEscapePatterns {
		unescaped = strings.ReplaceAll(unescaped, from, to)
	}

	if runtime.GOOS != "windows" {
		unescaped = strings.ReplaceAll(unescaped, `\\\\`, `\\`)
	}

	return unescaped
}

func GitLFSTrackReadOnly(ctx context.Context, path string) (bool, error) {
	_, err := GitLFSTrack(ctx, path)
	if err != nil {
		return false, fmt.Errorf("git lfs track failed: %w", err)
	}

	repoRoot, err := gitrepo.GitTopLevel()
	if err != nil {
		return false, err
	}

	attrPath := filepath.Join(repoRoot, ".gitattributes")
	changed, err := UpsertDRSRouteLines(attrPath, "ro", []string{path})
	if err != nil {
		return false, err
	}

	return changed, nil
}

func gitRevParseGitCommonDir(ctx context.Context) (string, error) {
	out, err := runGit(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir failed: %w", err)
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", errors.New("git rev-parse returned empty --git-common-dir")
	}
	// If relative, resolve it against current working directory.
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
	}
	return dir, nil
}

// GetGitRootDirectories
// returns (gitCommonDir, lfsRoot, error).
func GetGitRootDirectories(ctx context.Context) (string, string, error) {
	gitCommonDir, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		return "", "", err
	}
	lfsRoot, err := resolveLFSRoot(ctx, gitCommonDir)
	if err != nil {
		return "", "", err
	}
	if lfsRoot == "" {
		lfsRoot = filepath.Join(gitCommonDir, "lfs")
	}
	return gitCommonDir, lfsRoot, nil
}
