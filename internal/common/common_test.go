package common

import (
	"os"
	"strings"
	"testing"
)

func TestAuthzMapFromOrgProject(t *testing.T) {
	m := AuthzMapFromOrgProject("myorg", "myproject")
	if len(m) != 1 {
		t.Fatalf("expected 1 org key, got %d", len(m))
	}
	projs, ok := m["myorg"]
	if !ok || len(projs) != 1 || projs[0] != "myproject" {
		t.Fatalf("unexpected map: %v", m)
	}

	orgWide := AuthzMapFromOrgProject("myorg", "")
	if projs2, ok2 := orgWide["myorg"]; !ok2 || len(projs2) != 0 {
		t.Fatalf("expected org-wide (empty projects), got %v", orgWide)
	}

	if AuthzMapFromOrgProject("", "anything") != nil {
		t.Fatalf("expected nil when org is empty")
	}
}

func TestObjectBuilderDoesNotSynthesizeGen3StoragePrefix(t *testing.T) {
	obj, err := BuildDrsObjWithOptions("file.txt", "abc123", 10, "drs-1", ObjectLocationOptions{
		Bucket:       "bucket",
		Organization: "org",
		Project:      "proj",
	})
	if err != nil {
		t.Fatalf("BuildDrsObjWithOptions returned error: %v", err)
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) != 1 {
		t.Fatalf("expected access method, got %+v", obj.AccessMethods)
	}
	if got := (*obj.AccessMethods)[0].AccessUrl.Url; got != "s3://bucket/abc123" {
		t.Fatalf("unexpected access url: %q", got)
	}

	obj, err = BuildDrsObjWithOptions("file.txt", "def456", 10, "drs-2", ObjectLocationOptions{
		Bucket:        "bucket",
		Organization:  "org",
		Project:       "proj",
		StoragePrefix: "program-root/project-subpath",
	})
	if err != nil {
		t.Fatalf("BuildDrsObjWithOptions with prefix returned error: %v", err)
	}
	if got := (*obj.AccessMethods)[0].AccessUrl.Url; got != "s3://bucket/program-root/project-subpath/def456" {
		t.Fatalf("unexpected prefixed access url: %q", got)
	}
}

func TestAuthzMatchesScope(t *testing.T) {
	m := map[string][]string{"org": {"proj1", "proj2"}}
	if !AuthzMatchesScope(m, "org", "proj1") {
		t.Fatal("expected match for org/proj1")
	}
	if AuthzMatchesScope(m, "org", "other") {
		t.Fatal("expected no match for org/other")
	}
	orgWide := map[string][]string{"org": {}}
	if !AuthzMatchesScope(orgWide, "org", "anything") {
		t.Fatal("expected org-wide match")
	}
	if AuthzMatchesScope(nil, "org", "proj") {
		t.Fatal("expected no match for nil map")
	}
}

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
