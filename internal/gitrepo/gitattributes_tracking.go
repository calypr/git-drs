package gitrepo

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func TrackPatterns(_ context.Context, patterns []string, verbose bool, dryRun bool) (string, error) {
	return trackPatternsAtPath(patterns, verbose, dryRun, ".gitattributes", false)
}

func trackPatternsAtPath(patterns []string, verbose bool, dryRun bool, attributesPath string, literal bool) (string, error) {
	changedAttribLines := make(map[string]string, len(patterns))
	var output strings.Builder

	attribContents, err := readGitAttributes(attributesPath)
	if err != nil {
		return "", fmt.Errorf("git drs track failed: %w", err)
	}

	knownPatterns := parseKnownLFSPatterns(attribContents)

	for _, unsanitizedPattern := range patterns {
		pattern := trimCurrentPrefix(cleanRootPath(unsanitizedPattern))
		encodedArg := escapeAttrPattern(pattern)
		if literal {
			encodedArg = escapeLiteralAttrPattern(pattern)
		}
		lookupKey := attributePatternKey(encodedArg)

		if knownLine, ok := knownPatterns[lookupKey]; ok {
			knownPattern, _ := splitAttributeLine(knownLine)
			if knownPattern == encodedArg && strings.Contains(knownLine, "filter=drs") && strings.Contains(knownLine, "diff=drs") && strings.Contains(knownLine, "merge=drs") && strings.Contains(knownLine, "-text") {
				output.WriteString(fmt.Sprintf("%q already supported\n", pattern))
				continue
			}
		}

		changedAttribLines[lookupKey] = fmt.Sprintf("%s filter=drs diff=drs merge=drs -text", encodedArg)
		output.WriteString(fmt.Sprintf("Tracking %q\n", unescapeAttrPattern(encodedArg)))

		if verbose {
			output.WriteString(fmt.Sprintf("Searching for files matching pattern: %s\n", pattern))
			output.WriteString(fmt.Sprintf("Found %d files previously added to Git matching pattern: %s\n", 0, pattern))
		}
	}

	if !dryRun {
		if err := writeMergedGitAttributes(attributesPath, attribContents, changedAttribLines, false); err != nil {
			return "", fmt.Errorf("git drs track failed: %w", err)
		}
	}

	return output.String(), nil
}

func ListTrackedPatterns(_ context.Context, _ bool) (string, error) {
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
		pattern, _ := splitAttributeLine(line)
		if pattern == "" {
			continue
		}
		patterns = append(patterns, attributePatternKey(pattern))
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

func UntrackPatterns(_ context.Context, patterns []string, _ bool, dryRun bool) (string, error) {
	attribContents, err := readLocalGitAttributes()
	if err != nil {
		return "", fmt.Errorf("git drs untrack failed: %w", err)
	}
	if len(attribContents) == 0 {
		return "", nil
	}

	removeSet := make(map[string]string, len(patterns))
	for _, p := range patterns {
		path := trimCurrentPrefix(cleanRootPath(p))
		removeSet[escapeAttrPattern(path)] = path
		removeSet[escapeLiteralAttrPattern(path)] = path
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

		pattern, _ := splitAttributeLine(line)
		if pattern == "" {
			keptLines = append(keptLines, line)
			continue
		}

		path := trimCurrentPrefix(pattern)
		if original, ok := removeSet[path]; ok {
			out.WriteString(fmt.Sprintf("Untracking %q\n", original))
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

func TrackReadOnly(ctx context.Context, path string) (bool, error) {
	return trackReadOnly(ctx, path, true)
}

func TrackReadOnlyPattern(ctx context.Context, pattern string) (bool, error) {
	return trackReadOnly(ctx, pattern, false)
}

func trackReadOnly(ctx context.Context, path string, literal bool) (bool, error) {
	repoRoot, err := GitTopLevel()
	if err != nil {
		return false, err
	}

	attrPath := filepath.Join(repoRoot, ".gitattributes")
	if _, err := trackPatternsAtPath([]string{path}, false, false, attrPath, literal); err != nil {
		return false, fmt.Errorf("git lfs track failed: %w", err)
	}

	encoded := escapeAttrPattern(path)
	if literal {
		encoded = escapeLiteralAttrPattern(path)
	}
	changed, err := UpsertDRSRouteLines(attrPath, "ro", []string{encoded})
	if err != nil {
		return false, err
	}

	return changed, nil
}

func readLocalGitAttributes() ([]byte, error) {
	return readGitAttributes(".gitattributes")
}

func readGitAttributes(attributesPath string) ([]byte, error) {
	data, err := os.ReadFile(attributesPath)
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
		pattern, _ := splitAttributeLine(line)
		if pattern == "" {
			continue
		}
		known[attributePatternKey(pattern)] = line
	}
	return known
}

func writeMergedGitAttributes(attributesPath string, existing []byte, changed map[string]string, dryRun bool) error {
	if dryRun {
		return nil
	}

	var merged []string
	if len(existing) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(existing))
		for scanner.Scan() {
			line := scanner.Text()
			pattern, _ := splitAttributeLine(line)
			if pattern != "" {
				pat := attributePatternKey(pattern)
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
	if err := os.WriteFile(attributesPath, []byte(content), 0o644); err != nil {
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

func escapeLiteralAttrPattern(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '\\', '#', '!':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	encoded := b.String()
	if strings.ContainsAny(s, " \t\r\n") {
		return strconv.Quote(encoded)
	}
	return encoded
}

func attributePatternKey(pattern string) string {
	if strings.HasPrefix(pattern, "\"") {
		if decoded, err := strconv.Unquote(pattern); err == nil {
			pattern = decoded
		}
	}
	return unescapeAttrPattern(pattern)
}

func splitAttributeLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	if line[0] != '"' {
		fields := strings.Fields(line)
		return fields[0], fields[1:]
	}
	escaped := false
	for i := 1; i < len(line); i++ {
		switch {
		case escaped:
			escaped = false
		case line[i] == '\\':
			escaped = true
		case line[i] == '"':
			return line[:i+1], strings.Fields(line[i+1:])
		}
	}
	return "", nil
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
