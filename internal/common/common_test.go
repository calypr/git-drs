package common

import (
	"os"
	"strings"
	"testing"
)

func TestProjectToResource(t *testing.T) {
	// Test project-only format (no org)
	resource, err := ProjectToResource("", "prog-project")
	if err != nil {
		t.Fatalf("ProjectToResource error: %v", err)
	}
	if resource != "/programs/prog/projects/project" {
		t.Fatalf("unexpected resource: %s", resource)
	}

	// Test new format (with org)
	resource, err = ProjectToResource("myorg", "myproject")
	if err != nil {
		t.Fatalf("ProjectToResource error: %v", err)
	}
	if resource != "/programs/myorg/projects/myproject" {
		t.Fatalf("unexpected resource: %s", resource)
	}

	// Test project-only fallback (invalid format -> default program)
	res, err := ProjectToResource("", "invalid")
	if err != nil {
		t.Fatalf("unexpected error for invalid project: %v", err)
	}
	if res != "/programs/default/projects/invalid" {
		t.Fatalf("expected /programs/default/projects/invalid, got %s", res)
	}
}

func TestAddUnique(t *testing.T) {
	out := AddUnique([]string{"a", "b"}, []string{"b", "c"})
	if len(out) != 3 {
		t.Fatalf("expected 3 unique items, got %d", len(out))
	}
	if out[2] != "c" {
		t.Fatalf("expected new item appended, got %v", out)
	}
}

func TestCalculateFileSHA256(t *testing.T) {
	t.Run("returns checksum for file content", func(t *testing.T) {
		tmp, err := os.CreateTemp(t.TempDir(), "sha256-*.txt")
		if err != nil {
			t.Fatalf("CreateTemp error: %v", err)
		}
		defer tmp.Close()

		if _, err := tmp.WriteString("hello world\n"); err != nil {
			t.Fatalf("WriteString error: %v", err)
		}

		got, err := CalculateFileSHA256(tmp.Name())
		if err != nil {
			t.Fatalf("CalculateFileSHA256 error: %v", err)
		}

		const want = "a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447"
		if got != want {
			t.Fatalf("unexpected checksum: got %s, want %s", got, want)
		}
	})

	t.Run("returns error for missing file", func(t *testing.T) {
		_, err := CalculateFileSHA256("/path/that/does/not/exist")
		if err == nil {
			t.Fatalf("expected error for missing file")
		}
		if !strings.Contains(err.Error(), "open file") {
			t.Fatalf("expected open file error, got %v", err)
		}
	})
}
