package lfs

import (
	"strings"
	"testing"
)

func TestParseGitAttributes(t *testing.T) {
	content := `
# This is a comment
*.bin filter=lfs diff=lfs merge=lfs -text
*.zip filter=lfs diff=lfs merge=lfs -text
large-file.txt filter=lfs diff=lfs merge=lfs -text
*.txt text
docs/**/*.pdf filter=lfs

# Another comment
*.log -filter
`

	attrs, err := ParseGitAttributes(content)
	if err != nil {
		t.Fatalf("ParseGitAttributes failed: %v", err)
	}

	expected := []GitAttribute{
		{
			Pattern: "*.bin",
			Attributes: map[string]string{
				"filter": "lfs",
				"diff":   "lfs",
				"merge":  "lfs",
				"text":   "false",
			},
		},
		{
			Pattern: "*.zip",
			Attributes: map[string]string{
				"filter": "lfs",
				"diff":   "lfs",
				"merge":  "lfs",
				"text":   "false",
			},
		},
		{
			Pattern: "large-file.txt",
			Attributes: map[string]string{
				"filter": "lfs",
				"diff":   "lfs",
				"merge":  "lfs",
				"text":   "false",
			},
		},
		{
			Pattern: "*.txt",
			Attributes: map[string]string{
				"text": "true",
			},
		},
		{
			Pattern: "docs/**/*.pdf",
			Attributes: map[string]string{
				"filter": "lfs",
			},
		},
		{
			Pattern: "*.log",
			Attributes: map[string]string{
				"filter": "false",
			},
		},
	}

	if len(attrs) != len(expected) {
		t.Fatalf("Expected %d attributes, got %d", len(expected), len(attrs))
	}

	for i, attr := range attrs {
		// Verify attributes match
		for key, value := range expected[i].Attributes {
			if attr.Attributes[key] != value {
				t.Errorf("Attributes for %d mismatch: Expected %s=%s, got %s=%s", i, key, value, key, attr.Attributes[key])
			}
		}

		// Verify matcher works as expected
		if attr.matcher == nil {
			t.Errorf("Matcher for %d is nil", i)
			continue
		}

		// quick verification based on expected pattern
		var sampleFile string
		switch expected[i].Pattern {
		case "*.bin":
			sampleFile = "test.bin"
		case "*.zip":
			sampleFile = "test.zip"
		case "large-file.txt":
			sampleFile = "large-file.txt"
		case "*.txt":
			sampleFile = "doc.txt"
		case "docs/**/*.pdf":
			sampleFile = "docs/guide.pdf"
		case "*.log":
			sampleFile = "error.log"
		}

		if sampleFile != "" {
			parts := strings.Split(sampleFile, "/")
			if !attr.matcher.Match(parts) {
				t.Errorf("Matcher for pattern %s failed to match %s", expected[i].Pattern, sampleFile)
			}
		}
	}
}

func TestIsLFSTracked2(t *testing.T) {
	content := `
# LFS tracking
*.bin filter=lfs diff=lfs merge=lfs -text
*.zip filter=lfs diff=lfs merge=lfs -text
large-file.txt filter=lfs diff=lfs merge=lfs -text
docs/**/*.pdf filter=lfs
path/to/*.bin filter=lfs

# Regular text files
*.txt text
*.md text
`

	tests := []struct {
		filePath string
		expected bool
	}{
		// LFS tracked files (filename only matches)
		{"file.bin", true},       // *.bin matches filename
		{"archive.zip", true},    // *.zip matches filename
		{"large-file.txt", true}, // exact match

		// Files in subdirectories - should match simple glob patterns
		{"path/to/file.bin", true},   // This matches path/to/*.bin or *.bin
		{"other/dir/file.bin", true}, // This matches *.bin
		{"subdir/archive.zip", true}, // This matches *.zip

		// Double star patterns should match
		{"docs/manual/guide.pdf", true},
		{"docs/api/reference.pdf", true},

		// Files not matching any pattern
		{"file.exe", false},
		{"script.py", false},
	}

	for _, test := range tests {
		result, err := isLFSTracked(content, test.filePath)
		if err != nil {
			t.Errorf("isLFSTracked failed for %s: %v", test.filePath, err)
		}

		if result != test.expected {
			t.Errorf("isLFSTracked(%s) = %v, expected %v", test.filePath, result, test.expected)
		}
	}
}

// matchesViaGoGit is a helper to test pattern matching using go-git's logic
func matchesViaGoGit(pattern, filePath string) (bool, error) {
	// Create a minimal gitattributes content with the pattern
	content := pattern + " test=true"
	attrs, err := ParseGitAttributes(content)
	if err != nil {
		return false, err
	}
	if len(attrs) == 0 {
		return false, nil
	}

	// Use the matcher from the parsed attribute
	matcher := attrs[0].matcher
	if matcher == nil {
		return false, nil
	}

	parts := strings.Split(filePath, "/")
	return matcher.Match(parts), nil
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		filePath string
		expected bool
	}{
		// Exact matches
		{"large-file.txt", "large-file.txt", true},
		{"src/main.go", "src/main.go", true},

		// Simple glob patterns (filename only)
		{"*.txt", "readme.txt", true},
		{"*.txt", "file.txt", true},
		{"*.txt", "file.md", false},
		{"*.bin", "file.bin", true},
		{"*.bin", "path/to/file.bin", true},

		// Patterns with path separators
		{"src/*.go", "src/main.go", true},
		{"src/*.go", "main.go", false},
		{"path/to/*.bin", "path/to/file.bin", true},
		{"path/to/*.bin", "other/file.bin", false},

		// Directory patterns
		// "docs/" pattern causes panic in go-git v5.12.0 (handled by safeMatcher to return false)
		// {"docs/", "docs/readme.txt", true},
		// {"docs/", "src/main.go", false},

		// Double star patterns
		{"docs/**/*.pdf", "docs/manual/guide.pdf", true},
		{"docs/**/*.pdf", "docs/guide.pdf", true},
		{"docs/**/*.pdf", "src/guide.pdf", false},
		{"**/*.bin", "any/path/file.bin", true},
		{"**/*.bin", "file.bin", true},
	}

	for _, test := range tests {
		result, err := matchesViaGoGit(test.pattern, test.filePath)
		if err != nil {
			t.Errorf("matchesViaGoGit error: %v", err)
			continue
		}

		if result != test.expected {
			t.Errorf("matchesPattern(%s, %s) = %v, expected %v",
				test.pattern, test.filePath, result, test.expected)
		}
	}
}

func TestEdgeCases(t *testing.T) {
	// Test empty content
	result, err := isLFSTracked("", "file.txt")
	if err != nil {
		t.Errorf("isLFSTracked with empty content failed: %v", err)
	}
	if result {
		t.Error("Expected false for empty .gitattributes content")
	}

	// Test malformed content
	content := `
*.bin filter=lfs
invalid line without attributes
*.txt text
`
	result, err = isLFSTracked(content, "file.bin")
	if err != nil {
		t.Errorf("isLFSTracked with malformed content failed: %v", err)
	}
	if !result {
		t.Error("Expected true for file.bin with LFS filter")
	}
}

func TestOverrideRules(t *testing.T) {
	// Test that later rules override earlier ones
	content := `
*.bin filter=lfs
temp/*.bin -filter
`

	// File in temp directory should not be LFS tracked (overridden)
	result, err := isLFSTracked(content, "temp/data.bin")
	if err != nil {
		t.Errorf("isLFSTracked failed: %v", err)
	}
	if result {
		t.Error("Expected false for temp/data.bin (should be overridden)")
	}

	// File not in temp directory should be LFS tracked (filename match only)
	result, err = isLFSTracked(content, "data.bin")
	if err != nil {
		t.Errorf("isLFSTracked failed: %v", err)
	}
	if !result {
		t.Error("Expected true for data.bin")
	}

	// File in other subdirectory should be LFS tracked (*.bin matches anywhere)
	result, err = isLFSTracked(content, "src/data.bin")
	if err != nil {
		t.Errorf("isLFSTracked failed: %v", err)
	}
	if !result {
		t.Error("Expected true for src/data.bin (*.bin should match anywhere)")
	}
}

func TestGitAttributesBehavior(t *testing.T) {
	// Test realistic .gitattributes patterns
	content := `
# Track large files with LFS
*.psd filter=lfs diff=lfs merge=lfs -text
*.zip filter=lfs diff=lfs merge=lfs -text
*.7z filter=lfs diff=lfs merge=lfs -text

# Track specific file types in specific directories
assets/**/*.png filter=lfs diff=lfs merge=lfs -text
models/*.blend filter=lfs diff=lfs merge=lfs -text

# Text files
*.txt text
*.md text eol=lf
`

	tests := []struct {
		filePath string
		expected bool
		desc     string
	}{
		{"image.psd", true, "PSD file in root should be LFS"},
		{"project/image.psd", true, "PSD file in subdirectory should match *.psd"},
		{"archive.zip", true, "ZIP file in root should be LFS"},
		{"backup/archive.zip", true, "ZIP file in subdirectory should match *.zip"},
		{"assets/textures/logo.png", true, "PNG in assets should match assets/**/*.png"},
		{"images/logo.png", false, "PNG outside assets should NOT match"},
		{"models/character.blend", true, "Blend file in models should be LFS"},
		{"other/character.blend", false, "Blend file outside models should NOT match"},
		{"readme.txt", false, "Text file should not be LFS"},
		{"docs.md", false, "Markdown should not be LFS"},
	}

	for _, test := range tests {
		result, err := isLFSTracked(content, test.filePath)
		if err != nil {
			t.Errorf("isLFSTracked failed for %s: %v", test.filePath, err)
		}

		if result != test.expected {
			t.Errorf("isLFSTracked(%s) = %v, expected %v (%s)",
				test.filePath, result, test.expected, test.desc)
		}
	}
}
