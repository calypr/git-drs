package common

import "testing"

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
