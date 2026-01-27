package utils

import (
	"path/filepath"
	"testing"
)

func TestIsValidSHA256(t *testing.T) {
	tests := []struct {
		name  string
		hash  string
		valid bool
	}{
		{
			name:  "valid 64-char hex lowercase",
			hash:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			valid: true,
		},
		{
			name:  "valid 64-char hex uppercase",
			hash:  "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
			valid: true,
		},
		{
			name:  "too short",
			hash:  "abc123",
			valid: false,
		},
		{
			name:  "too long",
			hash:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8551",
			valid: false,
		},
		{
			name:  "invalid characters",
			hash:  "g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			valid: false,
		},
		{
			name:  "empty string",
			hash:  "",
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidSHA256(tt.hash)
			if result != tt.valid {
				t.Errorf("IsValidSHA256(%q) = %v, want %v", tt.hash, result, tt.valid)
			}
		})
	}
}

func TestPathOperations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "test/path",
			expected: "test/path",
		},
		{
			name:     "path with dots",
			input:    "test/../path",
			expected: "path",
		},
		{
			name:     "empty path",
			input:    "",
			expected: ".",
		},
		{
			name:     "absolute path",
			input:    "/usr/local/bin",
			expected: "/usr/local/bin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filepath.Clean(tt.input)
			if result != tt.expected {
				t.Errorf("filepath.Clean(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStringContains(t *testing.T) {
	slice := []string{"apple", "banana", "cherry", "date"}

	tests := []struct {
		name   string
		search string
		found  bool
	}{
		{"exists at start", "apple", true},
		{"exists in middle", "banana", true},
		{"exists at end", "date", true},
		{"does not exist", "orange", false},
		{"empty string", "", false},
		{"case sensitive", "Apple", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, item := range slice {
				if item == tt.search {
					found = true
					break
				}
			}
			if found != tt.found {
				t.Errorf("String %q in slice = %v, want %v", tt.search, found, tt.found)
			}
		})
	}
}

func TestFilePathJoin(t *testing.T) {
	tests := []struct {
		name     string
		parts    []string
		expected string
	}{
		{
			name:     "two parts",
			parts:    []string{"dir", "file.txt"},
			expected: "dir/file.txt",
		},
		{
			name:     "three parts",
			parts:    []string{"dir1", "dir2", "file.txt"},
			expected: "dir1/dir2/file.txt",
		},
		{
			name:     "with dots",
			parts:    []string{"dir", "..", "file.txt"},
			expected: "file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filepath.Join(tt.parts...)
			result = filepath.Clean(result)
			if result != tt.expected {
				t.Errorf("filepath.Join(%v) = %q, want %q", tt.parts, result, tt.expected)
			}
		})
	}
}
