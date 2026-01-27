package drsmap

import (
	"testing"

	drs "github.com/calypr/data-client/indexd/drs"
)

// Test DrsUUID function - it's already tested but let's add more coverage
func TestDrsUUID_Consistency(t *testing.T) {
	// Test that DrsUUID generates consistent UUIDs for same inputs
	projectID := "test-project"
	hash := "abc123"

	uuid1 := DrsUUID(projectID, hash)
	uuid2 := DrsUUID(projectID, hash)

	if uuid1 != uuid2 {
		t.Errorf("DrsUUID should be deterministic, got %s and %s", uuid1, uuid2)
	}

	if uuid1 == "" {
		t.Error("DrsUUID should not return empty string")
	}
}

func TestDrsUUID_DifferentInputs(t *testing.T) {
	// Test that different inputs produce different UUIDs
	uuid1 := DrsUUID("project1", "hash1")
	uuid2 := DrsUUID("project2", "hash1")
	uuid3 := DrsUUID("project1", "hash2")

	if uuid1 == uuid2 {
		t.Error("Different projects should produce different UUIDs")
	}

	if uuid1 == uuid3 {
		t.Error("Different hashes should produce different UUIDs")
	}
}

func TestLfsDryRunSpec_Validation(t *testing.T) {
	tests := []struct {
		name    string
		spec    LfsDryRunSpec
		isValid bool
	}{
		{
			name:    "valid spec",
			spec:    LfsDryRunSpec{Remote: "origin", Ref: "main"},
			isValid: true,
		},
		{
			name:    "missing remote",
			spec:    LfsDryRunSpec{Remote: "", Ref: "main"},
			isValid: false,
		},
		{
			name:    "missing ref",
			spec:    LfsDryRunSpec{Remote: "origin", Ref: ""},
			isValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.spec.Remote != "" && tt.spec.Ref != ""
			if valid != tt.isValid {
				t.Errorf("Expected validity %v, got %v", tt.isValid, valid)
			}
		})
	}
}

func TestLfsFileInfo_Fields(t *testing.T) {
	info := LfsFileInfo{
		Name:       "test.bin",
		Size:       2048,
		Checkout:   true,
		Downloaded: false,
		OidType:    "sha256",
		Oid:        "abc123def456",
		Version:    "1.0",
	}

	if info.Name != "test.bin" {
		t.Errorf("Expected Name 'test.bin', got %s", info.Name)
	}
	if info.Size != 2048 {
		t.Errorf("Expected Size 2048, got %d", info.Size)
	}
	if !info.Checkout {
		t.Error("Expected Checkout to be true")
	}
	if info.Downloaded {
		t.Error("Expected Downloaded to be false")
	}
	if info.OidType != "sha256" {
		t.Errorf("Expected OidType 'sha256', got %s", info.OidType)
	}
}

func TestFindMatchingRecord_EmptyList(t *testing.T) {
	// Test with empty list
	result, err := FindMatchingRecord([]drs.DRSObject{}, "test-project")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result for empty list")
	}
}

func TestCreateCustomPath_BasicCases(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
		drsURI  string
		wantErr bool
	}{
		{
			name:    "valid DRS URI",
			baseDir: "/tmp",
			drsURI:  "drs://namespace:id123",
			wantErr: false,
		},
		{
			name:    "empty base dir",
			baseDir: "",
			drsURI:  "drs://namespace:id123",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := CreateCustomPath(tt.baseDir, tt.drsURI)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateCustomPath() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && path == "" {
				t.Error("Expected non-empty path")
			}
		})
	}
}

func TestCreateCustomPath_InvalidCases(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
		drsURI  string
	}{
		{
			name:    "invalid prefix",
			baseDir: "/tmp",
			drsURI:  "http://namespace:id123",
		},
		{
			name:    "short URI",
			baseDir: "/tmp",
			drsURI:  "drs://",
		},
		{
			name:    "missing colon",
			baseDir: "/tmp",
			drsURI:  "drs://namespaceid123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CreateCustomPath(tt.baseDir, tt.drsURI)
			if err == nil {
				t.Errorf("CreateCustomPath(%q) expected error, got nil", tt.drsURI)
			}
		})
	}
}

func TestFindMatchingRecord_MatchFound(t *testing.T) {
	projectID := "PROG-PROJ"
	expectedAuthz := "/programs/PROG/projects/PROJ"

	records := []drs.DRSObject{
		{
			Id: "no-match",
			AccessMethods: []drs.AccessMethod{
				{
					Type: "s3",
					Authorizations: &drs.Authorizations{
						Value: "other-resource",
					},
				},
			},
		},
		{
			Id: "match",
			AccessMethods: []drs.AccessMethod{
				{
					Type: "s3",
					Authorizations: &drs.Authorizations{
						Value: expectedAuthz,
					},
				},
			},
		},
	}

	result, err := FindMatchingRecord(records, projectID)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected to find a matching record")
	}
	if result.Id != "match" {
		t.Errorf("Expected record Id 'match', got '%s'", result.Id)
	}
}

func TestFindMatchingRecord_NoMatch(t *testing.T) {
	projectID := "PROG-PROJ"

	records := []drs.DRSObject{
		{
			Id: "no-match",
			AccessMethods: []drs.AccessMethod{
				{
					Type: "s3",
					Authorizations: &drs.Authorizations{
						Value: "other-resource",
					},
				},
			},
		},
	}

	result, err := FindMatchingRecord(records, projectID)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result, got %v", result)
	}
}

func TestFindMatchingRecord_InvalidProjectID(t *testing.T) {
	projectID := "invalid" // Missing hyphen

	records := []drs.DRSObject{
		{
			Id: "any",
			AccessMethods: []drs.AccessMethod{
				{
					Type: "s3",
					Authorizations: &drs.Authorizations{
						Value: "any",
					},
				},
			},
		},
	}

	_, err := FindMatchingRecord(records, projectID)
	if err == nil {
		t.Error("Expected error for invalid project ID")
	}
}
