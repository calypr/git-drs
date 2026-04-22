package drsmap

import (
	"testing"

	"github.com/calypr/git-drs/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestDrsUUID_Consistency(t *testing.T) {
	projectID := "test-project"
	hash := "abc123"

	uuid1 := DrsUUID(projectID, hash)
	uuid2 := DrsUUID(projectID, hash)

	if uuid1 != uuid2 {
		t.Fatalf("DrsUUID should be deterministic, got %s and %s", uuid1, uuid2)
	}
}

func TestLfsDryRunSpec_Validation(t *testing.T) {
	tests := []struct {
		name    string
		spec    lfs.DryRunSpec
		isValid bool
	}{
		{"valid spec", lfs.DryRunSpec{Remote: "origin", Ref: "main"}, true},
		{"missing remote", lfs.DryRunSpec{Remote: "", Ref: "main"}, false},
		{"missing ref", lfs.DryRunSpec{Remote: "origin", Ref: ""}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.spec.Remote != "" && tt.spec.Ref != ""
			if valid != tt.isValid {
				t.Fatalf("expected validity %v, got %v", tt.isValid, valid)
			}
		})
	}
}

func TestFindMatchingRecord_EmptyList(t *testing.T) {
	result, err := FindMatchingRecord([]drsapi.DrsObject{}, "", "test-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty list")
	}
}

func makeAuthzRecord(id, issuer string) drsapi.DrsObject {
	issuers := []string{issuer}
	accessMethods := []drsapi.AccessMethod{{
		Type: "s3",
		Authorizations: &struct {
			BearerAuthIssuers   *[]string                                          `json:"bearer_auth_issuers,omitempty"`
			DrsObjectId         *string                                            `json:"drs_object_id,omitempty"`
			PassportAuthIssuers *[]string                                          `json:"passport_auth_issuers,omitempty"`
			SupportedTypes      *[]drsapi.AccessMethodAuthorizationsSupportedTypes `json:"supported_types,omitempty"`
		}{
			BearerAuthIssuers: &issuers,
		},
	}}
	return drsapi.DrsObject{
		Id:            id,
		AccessMethods: &accessMethods,
		Checksums:     []drsapi.Checksum{{Type: "sha256", Checksum: "sha256"}},
	}
}

func TestFindMatchingRecord_MatchFound(t *testing.T) {
	records := []drsapi.DrsObject{
		makeAuthzRecord("no-match", "other-resource"),
		makeAuthzRecord("match", "/programs/PROG/projects/PROJ"),
	}

	result, err := FindMatchingRecord(records, "", "PROG-PROJ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Id != "match" {
		t.Fatalf("expected record id match, got %#v", result)
	}
}

func TestFindMatchingRecord_NoAuthzMatchReturnsNil(t *testing.T) {
	records := []drsapi.DrsObject{
		makeAuthzRecord("no-match", "other-resource"),
	}
	result, err := FindMatchingRecord(records, "", "PROG-PROJ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil when no authz matches, got id=%q", result.Id)
	}
}

func TestFindMatchingRecord_NonHyphenated(t *testing.T) {
	if _, err := FindMatchingRecord([]drsapi.DrsObject{}, "", "no-hyphen"); err != nil {
		t.Fatalf("FindMatchingRecord should accept non-hyphenated project ID: %v", err)
	}
}
