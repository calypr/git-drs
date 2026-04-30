package drstrack

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
func UpsertDRSRouteLines(gitattributesPath string, mode string, patterns []string) (changed bool, err error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "ro" && mode != "rw" {
		return false, fmt.Errorf("invalid mode %q", mode)
	}

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

	seen := make(map[string]int)
	for i, line := range lines {
		pat, _, ok := parseRouteLine(line)
		if ok {
			seen[pat] = i
		}
	}

	for _, pat := range order {
		newLine := fmt.Sprintf("%s drs.route=%s", pat, mode)
		if idx, ok := seen[pat]; ok {
			if strings.TrimSpace(lines[idx]) != newLine {
				lines[idx] = newLine
				changed = true
			}
			continue
		}
		lines = append(lines, newLine)
		changed = true
	}

	if !changed && readErr == nil {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(gitattributesPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir %s: %w", filepath.Dir(gitattributesPath), err)
	}

	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(gitattributesPath, []byte(out), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", gitattributesPath, err)
	}
	return true, nil
}

func parseRouteLine(line string) (pattern string, mode string, ok bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false
	}

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
			return "", "", false
		}
	}
	return "", "", false
}
