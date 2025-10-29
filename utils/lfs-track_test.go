package utils

import (
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
		if attr.Pattern != expected[i].Pattern {
			t.Errorf("Expected pattern %s, got %s", expected[i].Pattern, attr.Pattern)
		}

		for key, value := range expected[i].Attributes {
			if attr.Attributes[key] != value {
				t.Errorf("Expected %s=%s, got %s=%s", key, value, key, attr.Attributes[key])
			}
		}
	}
}

func TestIsLFSTracked(t *testing.T) {
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

		// Files in subdirectories - should NOT match simple glob patterns
		{"path/to/file.bin", true},    // This should match path/to/*.bin pattern
		{"other/dir/file.bin", false}, // This should NOT match *.bin (no path separator in pattern)
		{"subdir/archive.zip", false}, // This should NOT match *.zip

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
		{"*.bin", "path/to/file.bin", false}, // CORRECTED: Should be false

		// Patterns with path separators
		{"src/*.go", "src/main.go", true},
		{"src/*.go", "main.go", false},
		{"path/to/*.bin", "path/to/file.bin", true},
		{"path/to/*.bin", "other/file.bin", false},

		// Directory patterns
		{"docs/", "docs/readme.txt", true},
		{"docs/", "src/main.go", false},

		// Double star patterns
		{"docs/**/*.pdf", "docs/manual/guide.pdf", true},
		{"docs/**/*.pdf", "docs/guide.pdf", true},
		{"docs/**/*.pdf", "src/guide.pdf", false},
		{"**/*.bin", "any/path/file.bin", true},
		{"**/*.bin", "file.bin", true},
	}

	for _, test := range tests {
		result := matchesPattern(test.pattern, test.filePath)
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

	// File in other subdirectory should NOT be LFS tracked (*.bin only matches filename)
	result, err = isLFSTracked(content, "src/data.bin")
	if err != nil {
		t.Errorf("isLFSTracked failed: %v", err)
	}
	if result {
		t.Error("Expected false for src/data.bin (*.bin should only match filename)")
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
		{"project/image.psd", false, "PSD file in subdirectory should NOT match *.psd"},
		{"archive.zip", true, "ZIP file in root should be LFS"},
		{"backup/archive.zip", false, "ZIP file in subdirectory should NOT match *.zip"},
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
