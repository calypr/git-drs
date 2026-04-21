package common

import (
	"testing"
)

func TestProjectToResource(t *testing.T) {
	// Test legacy format (no org)
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

	// Test legacy format fallback (invalid format -> default program)
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
