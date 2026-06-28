package drslookup

import (
	"testing"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
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

func TestFindMatchingRecordEmptyList(t *testing.T) {
	result, err := FindMatchingRecord([]drsapi.DrsObject{}, "", "test-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty list")
	}
}

func makeScopedRecord(id, resource string) drsapi.DrsObject {
	controlledAccess := []string{resource}
	return drsapi.DrsObject{
		Id:               id,
		ControlledAccess: &controlledAccess,
		Checksums:        []drsapi.Checksum{{Type: "sha256", Checksum: "sha256"}},
	}
}

func TestFindMatchingRecordMatchFound(t *testing.T) {
	records := []drsapi.DrsObject{
		makeScopedRecord("no-match", "/organization/OTHER/project/resource"),
		makeScopedRecord("match", "/organization/PROG/project/PROJ"),
	}

	result, err := FindMatchingRecord(records, "", "PROG-PROJ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Id != "match" {
		t.Fatalf("expected controlled_access record match, got %#v", result)
	}
}

func TestFindMatchingRecordNoControlledAccessMatchReturnsNil(t *testing.T) {
	records := []drsapi.DrsObject{
		makeScopedRecord("no-match", "/organization/OTHER/project/resource"),
	}
	result, err := FindMatchingRecord(records, "", "PROG-PROJ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil when no controlled_access matches, got id=%q", result.Id)
	}
}

func TestFindMatchingRecordNonHyphenated(t *testing.T) {
	if _, err := FindMatchingRecord([]drsapi.DrsObject{}, "", "no-hyphen"); err != nil {
		t.Fatalf("FindMatchingRecord should accept non-hyphenated project ID: %v", err)
	}
}
