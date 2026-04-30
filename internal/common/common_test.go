package common

import (
	"os"
	"strings"
	"testing"
)

func TestParseOrgProject(t *testing.T) {
	org, proj := ParseOrgProject("", "prog-project")
	if org != "prog" || proj != "project" {
		t.Fatalf("expected prog/project, got %s/%s", org, proj)
	}
	org, proj = ParseOrgProject("myorg", "myproject")
	if org != "myorg" || proj != "myproject" {
		t.Fatalf("expected myorg/myproject, got %s/%s", org, proj)
	}
	org, proj = ParseOrgProject("", "nohyphen")
	if org != "default" || proj != "nohyphen" {
		t.Fatalf("expected default/nohyphen, got %s/%s", org, proj)
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
