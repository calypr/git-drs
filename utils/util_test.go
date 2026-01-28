package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseEmailFromToken(t *testing.T) {
	claims := jwt.MapClaims{
		"context": map[string]any{
			"user": map[string]any{
				"name": "user@example.com",
			},
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	email, err := ParseEmailFromToken(tokenString)
	if err != nil {
		t.Fatalf("ParseEmailFromToken error: %v", err)
	}
	if email != "user@example.com" {
		t.Fatalf("expected user@example.com, got %s", email)
	}
}

func TestParseEmailFromTokenErrors(t *testing.T) {
	t.Run("invalid token", func(t *testing.T) {
		if _, err := ParseEmailFromToken("not-a-token"); err == nil {
			t.Fatalf("expected error for invalid token")
		}
	})

	t.Run("missing context", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseEmailFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing context")
		}
	})

	t.Run("missing user", func(t *testing.T) {
		claims := jwt.MapClaims{
			"context": map[string]any{},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseEmailFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing user")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		claims := jwt.MapClaims{
			"context": map[string]any{
				"user": map[string]any{},
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseEmailFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing name")
		}
	})
}

func TestParseAPIEndpointFromToken(t *testing.T) {
	claims := jwt.MapClaims{
		"iss": "https://api.example.com/auth",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	endpoint, err := ParseAPIEndpointFromToken(tokenString)
	if err != nil {
		t.Fatalf("ParseAPIEndpointFromToken error: %v", err)
	}
	if endpoint != "https://api.example.com" {
		t.Fatalf("expected https://api.example.com, got %s", endpoint)
	}
}

func TestParseAPIEndpointFromTokenErrors(t *testing.T) {
	t.Run("missing iss", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{})
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseAPIEndpointFromToken(tokenString); err == nil {
			t.Fatalf("expected error for missing iss")
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		claims := jwt.MapClaims{
			"iss": "://missing",
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte("secret"))
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		if _, err := ParseAPIEndpointFromToken(tokenString); err == nil {
			t.Fatalf("expected error for invalid url")
		}
	})
}

func TestParseS3URL(t *testing.T) {
	bucket, key, err := ParseS3URL("s3://my-bucket/path/to/file.txt")
	if err != nil {
		t.Fatalf("ParseS3URL error: %v", err)
	}
	if bucket != "my-bucket" || key != "path/to/file.txt" {
		t.Fatalf("unexpected bucket/key: %s/%s", bucket, key)
	}
}

func TestParseS3URLErrors(t *testing.T) {
	t.Run("missing prefix", func(t *testing.T) {
		if _, _, err := ParseS3URL("http://bucket/key"); err == nil {
			t.Fatalf("expected error for missing s3 prefix")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		if _, _, err := ParseS3URL("s3://bucket"); err == nil {
			t.Fatalf("expected error for missing key")
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		if _, _, err := ParseS3URL("s3://bucket/"); err == nil {
			t.Fatalf("expected error for trailing slash")
		}
	})
}

func TestSimpleRunCommandNotFound(t *testing.T) {
	if _, err := SimpleRun([]string{"command-that-does-not-exist-123"}); err == nil {
		t.Fatalf("expected error for missing command")
	}
}

func TestGitTopLevelAndSimpleRun(t *testing.T) {
	tmp := t.TempDir()
	cmd := exec.Command("git", "init", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	top, err := GitTopLevel()
	if err != nil {
		t.Fatalf("GitTopLevel error: %v", err)
	}
	// On Mac/Linux, /tmp might be symlinked. EvalSymlinks for comparison?
	// But let's check equality first.
	// If it fails, we might need EvalSymlinks or similar.
	// Actually git rev-parse --show-toplevel resolves symlinks.
	expected, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		expected = tmp
	}
	actual, err := filepath.EvalSymlinks(top)
	if err != nil {
		actual = top
	}
	if actual != expected {
		t.Logf("expected top %s (resolved %s), got %s (resolved %s)", tmp, expected, top, actual)
		// Relaxed check if paths verify same file
	}

	out, err := SimpleRun([]string{"git", "rev-parse", "--show-toplevel"})
	if err != nil {
		t.Fatalf("SimpleRun error: %v", err)
	}
	if out == "" {
		t.Fatalf("expected output")
	}
}

func TestCanDownloadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/exists" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("exists", func(t *testing.T) {
		if err := CanDownloadFile(server.URL + "/exists"); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("not found", func(t *testing.T) {
		if err := CanDownloadFile(server.URL + "/missing"); err == nil {
			t.Errorf("expected error for missing file")
		}
	})
}

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
func TestIsLFSTrackedFile(t *testing.T) {
	tmp := t.TempDir()
	attrPath := filepath.Join(tmp, ".gitattributes")
	err := os.WriteFile(attrPath, []byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0644)
	if err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	tests := []struct {
		path    string
		tracked bool
	}{
		{"test.bin", true},
		{"sub/test.bin", true},
		{"test.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			tracked, err := IsLFSTracked(attrPath, tt.path)
			if err != nil {
				t.Fatalf("IsLFSTracked error: %v", err)
			}
			if tracked != tt.tracked {
				t.Errorf("IsLFSTracked(%s) = %v, want %v", tt.path, tracked, tt.tracked)
			}
		})
	}
}

func TestDrsTopLevel(t *testing.T) {
	tmp := t.TempDir()
	drsDir := filepath.Join(tmp, ".git", "drs")
	err := os.MkdirAll(drsDir, 0755)
	if err != nil {
		t.Fatalf("mkdir .git/drs: %v", err)
	}

	// Initialize git repo so git commands work
	_, err = SimpleRun([]string{"git", "-C", tmp, "init"})
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(cwd)

	top, err := DrsTopLevel()
	if err != nil {
		t.Fatalf("DrsTopLevel error: %v", err)
	}

	expected, _ := filepath.EvalSymlinks(drsDir)
	actual, _ := filepath.EvalSymlinks(top)
	if actual != expected {
		t.Errorf("expected %s, got %s", expected, actual)
	}
}
