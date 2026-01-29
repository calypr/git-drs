package lfs

import (
	"fmt"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitattributes"
)

// Matcher defines the interface for pattern matching
type Matcher interface {
	Match(path []string) bool
}

// safeMatcher wraps a Matcher to panic-safe execution
type safeMatcher struct {
	internal Matcher
}

func (m *safeMatcher) Match(path []string) (matched bool) {
	defer func() {
		if r := recover(); r != nil {
			matched = false
		}
	}()
	return m.internal.Match(path)
}

// GitAttribute represents a single line in .gitattributes file
type GitAttribute struct {
	Pattern    string
	Attributes map[string]string
	matcher    Matcher
}

// ParseGitAttributes parses the content of a .gitattributes file
func ParseGitAttributes(content string) ([]GitAttribute, error) {
	atrs, err := gitattributes.ReadAttributes(strings.NewReader(content), nil, false)
	if err != nil {
		return nil, err
	}

	var attributes []GitAttribute
	for _, atr := range atrs {
		attrMap := make(map[string]string)
		for _, a := range atr.Attributes {
			name := a.Name()
			if a.IsSet() {
				attrMap[name] = "true"
			} else if a.IsUnset() {
				attrMap[name] = "false"
			} else if a.IsValueSet() {
				attrMap[name] = a.Value()
			}
		}

		var matcher Matcher
		if m, ok := atr.Pattern.(Matcher); ok {
			matcher = &safeMatcher{internal: m}
		}

		// attributes.Pattern string is not easily available from go-git Pattern interface.
		// We leave it empty or store the raw string representation if useful for debugging
		patternStr := fmt.Sprintf("%v", atr.Pattern)

		attributes = append(attributes, GitAttribute{
			Pattern:    patternStr,
			Attributes: attrMap,
			matcher:    matcher,
		})
	}

	return attributes, nil
}

func IsLFSTracked(gitattributesFilePath, filePath string) (bool, error) {
	gitattributesContent, err := os.ReadFile(gitattributesFilePath)
	if err != nil {
		return false, fmt.Errorf("failed to read .gitattributes file: %w", err)
	}

	return isLFSTracked(string(gitattributesContent), filePath)
}

// isLFSTracked determines if a given file path is tracked by Git LFS
func isLFSTracked(gitattributesContent string, filePath string) (bool, error) {
	attributes, err := ParseGitAttributes(gitattributesContent)
	if err != nil {
		return false, err
	}

	isLFS := false

	// split path into parts for matching
	pathParts := strings.Split(filePath, "/")
	if os.PathSeparator != '/' && strings.Contains(filePath, string(os.PathSeparator)) {
		pathParts = strings.Split(filePath, string(os.PathSeparator))
	}

	for _, attr := range attributes {
		matched := false
		if attr.matcher != nil {
			matched = attr.matcher.Match(pathParts)
		}

		if matched {
			if filter, exists := attr.Attributes["filter"]; exists {
				isLFS = (filter == "lfs")
			}
		}
	}

	return isLFS, nil
}
