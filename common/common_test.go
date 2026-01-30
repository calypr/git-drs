package common

import (
	"testing"

	dataClientCommon "github.com/calypr/data-client/common"
)

func TestProjectToResource(t *testing.T) {
	resource, err := dataClientCommon.ProjectToResource("prog-project")
	if err != nil {
		t.Fatalf("ProjectToResource error: %v", err)
	}
	if resource != "/programs/prog/projects/project" {
		t.Fatalf("unexpected resource: %s", resource)
	}

	if _, err := dataClientCommon.ProjectToResource("invalid"); err == nil {
		t.Fatalf("expected error for invalid project")
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
