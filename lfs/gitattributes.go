package lfs

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UpsertDRSRouteLines adds or updates .gitattributes lines of the form:
//
//	<pattern> drs.route=<ro|rw>
//
// Returns changed=true if the file was modified.
func UpsertDRSRouteLines(gitattributesPath string, mode string, patterns []string) (changed bool, err error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "ro" && mode != "rw" {
		return false, fmt.Errorf("invalid mode %q", mode)
	}

	// Normalize patterns (preserve original spelling except trim).
	want := make(map[string]string, len(patterns))
	order := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := want[p]; !ok {
			want[p] = p
			order = append(order, p)
		}
	}
	if len(order) == 0 {
		return false, fmt.Errorf("no patterns provided")
	}

	// Read existing file if present.
	var lines []string
	data, readErr := os.ReadFile(gitattributesPath)
	if readErr == nil {
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		if err := sc.Err(); err != nil {
			return false, fmt.Errorf("read %s: %w", gitattributesPath, err)
		}
	} else if !os.IsNotExist(readErr) {
		return false, fmt.Errorf("read %s: %w", gitattributesPath, readErr)
	}

	// Build index of existing drs.route lines.
	// We match "pattern ... drs.route=<x>" in a whitespace-tolerant way, but only update
	// if the first token equals the pattern exactly.
	seen := make(map[string]int) // pattern -> line index
	for i, line := range lines {
		pat, _, ok := parseRouteLine(line)
		if ok {
			seen[pat] = i
		}
	}

	// Apply upserts.
	for _, pat := range order {
		newLine := fmt.Sprintf("%s drs.route=%s", pat, mode)

		if idx, ok := seen[pat]; ok {
			// Update only if different
			if strings.TrimSpace(lines[idx]) != newLine {
				lines[idx] = newLine
				changed = true
			}
		} else {
			lines = append(lines, newLine)
			changed = true
		}
	}

	if !changed && readErr == nil {
		return false, nil
	}

	// Ensure directory exists (it should, but be safe).
	if err := os.MkdirAll(filepath.Dir(gitattributesPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(gitattributesPath), err)
	}

	// Write back with trailing newline.
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(gitattributesPath, []byte(out), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", gitattributesPath, err)
	}
	return true, nil
}

// parseRouteLine returns (pattern, mode, ok) for lines like:
//
//	scratch/** drs.route=rw
//
// It ignores comments and blank lines.
func parseRouteLine(line string) (pattern string, mode string, ok bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false
	}

	// Tokenize by whitespace.
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return "", "", false
	}

	pat := parts[0]
	for _, tok := range parts[1:] {
		if strings.HasPrefix(tok, "drs.route=") {
			val := strings.TrimPrefix(tok, "drs.route=")
			val = strings.ToLower(strings.TrimSpace(val))
			if val == "ro" || val == "rw" {
				return pat, val, true
			}
			// present but invalid -> treat as not-ok to avoid “fixing” unknown formats silently
			return "", "", false
		}
	}
	return "", "", false
}
