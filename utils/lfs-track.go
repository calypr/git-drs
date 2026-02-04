package utils

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitAttribute represents a single line in .gitattributes file
type GitAttribute struct {
	Pattern    string
	Attributes map[string]string
}

// ParseGitAttributes parses the content of a .gitattributes file
func ParseGitAttributes(content string) ([]GitAttribute, error) {
	var attributes []GitAttribute
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		attr, err := parseLine(line)
		if err != nil {
			continue // Skip malformed lines
		}

		attributes = append(attributes, attr)
	}

	return attributes, scanner.Err()
}

// parseLine parses a single line from .gitattributes
func parseLine(line string) (GitAttribute, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return GitAttribute{}, nil
	}

	pattern := parts[0]
	attributes := make(map[string]string)

	for _, attr := range parts[1:] {
		if strings.Contains(attr, "=") {
			// Handle key=value attributes
			kv := strings.SplitN(attr, "=", 2)
			attributes[kv[0]] = kv[1]
		} else if strings.HasPrefix(attr, "-") {
			// Handle negated attributes (-attr)
			attributes[attr[1:]] = "false"
		} else {
			// Handle simple attributes (attr)
			attributes[attr] = "true"
		}
	}

	return GitAttribute{
		Pattern:    pattern,
		Attributes: attributes,
	}, nil
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

	// Process attributes in order, later rules override earlier ones
	isLFS := false

	for _, attr := range attributes {
		if matchesPattern(attr.Pattern, filePath) {
			// Check for LFS attributes
			if filter, exists := attr.Attributes["filter"]; exists {
				return filter == "lfs", nil // Return immediately on filter match
			}
		}
	}

	return isLFS, nil
}

// matchesPattern checks if a file path matches a gitattributes pattern
func matchesPattern(pattern, filePath string) bool {
	// Handle exact matches first
	if pattern == filePath {
		return true
	}

	// Handle directory patterns ending with /
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(filePath+"/", pattern)
	}

	// Handle ** patterns (match any number of directories)
	if strings.Contains(pattern, "**") {
		return matchesDoubleStarPattern(pattern, filePath)
	}

	// Handle patterns with explicit path separators
	if strings.Contains(pattern, "/") {
		// Pattern contains path separator, do full path matching
		matched, err := filepath.Match(pattern, filePath)
		if err != nil {
			return false
		}
		return matched
	}

	// For simple glob patterns without path separators (like *.bin),
	// only match against the filename, not the full path
	// filename := filepath.Base(filePath)
	matched, err := filepath.Match(pattern, filePath)
	if err != nil {
		return false
	}

	return matched
}

// matchesDoubleStarPattern handles ** patterns in gitattributes
func matchesDoubleStarPattern(pattern, filePath string) bool {
	// Handle ** patterns by splitting on ** and matching each part
	parts := strings.Split(pattern, "**")

	// If no ** found, fall back to regular matching
	if len(parts) == 1 {
		matched, err := filepath.Match(pattern, filePath)
		return err == nil && matched
	}

	// For patterns with **, we need to match each part
	// Example: "docs/**/*.pdf" becomes ["docs/", "/*.pdf"]
	// Example: "**/*.bin" becomes ["", "/*.bin"]

	currentPath := filePath

	for i, part := range parts {
		if part == "" {
			continue // Skip empty parts
		}

		if i == 0 {
			// First part - must match the beginning
			if strings.HasSuffix(part, "/") {
				// Directory prefix
				if !strings.HasPrefix(currentPath, part) {
					return false
				}
				currentPath = currentPath[len(part):]
			} else {
				// File pattern at the beginning
				matched, err := filepath.Match(part, currentPath)
				return err == nil && matched
			}
		} else if i == len(parts)-1 {
			// Last part - must match the end or remaining path
			part = strings.TrimPrefix(part, "/")

			// For the last part, try to match against the filename or remaining path
			if strings.Contains(part, "/") {
				// Pattern has path components, match against remaining path
				matched, err := filepath.Match(part, currentPath)
				return err == nil && matched
			} else {
				// Simple filename pattern, match against just the filename
				filename := filepath.Base(currentPath)
				matched, err := filepath.Match(part, filename)
				return err == nil && matched
			}
		}
		// Middle parts would be handled here if we had more complex patterns
	}

	return true
}
