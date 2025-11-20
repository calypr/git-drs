package client

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindEarliestCreatedRecordForProject(t *testing.T) {
	// Test with multiple records with different CreatedDate values
	// Note: authz format is /programs/<program>/projects/<project>
	// and projectId format is <program>-<project> (single hyphen separator)
	records := []OutputInfo{
		{
			Did:         "uuid-oldest",
			CreatedDate: "1970-01-01T00:16:40Z",
			Authz:       []string{"/programs/testprogram/projects/testproject"},
		},
		{
			Did:         "uuid-middle",
			CreatedDate: "1970-01-01T00:33:20Z",
			Authz:       []string{"/programs/testprogram/projects/testproject"},
		},
		{
			Did:         "uuid-newest",
			CreatedDate: "1970-01-01T00:50:00Z",
			Authz:       []string{"/programs/testprogram/projects/testproject"},
		},
	}

	canonical, err := FindEarliestCreatedRecordForProject(records, "testprogram-testproject")
	require.NoError(t, err)
	require.NotNil(t, canonical)
	require.Equal(t, "uuid-oldest", canonical.Did)
}

func TestFindEarliestCreatedRecordForProject_IndexdFormat(t *testing.T) {
	// Test with real indexd timestamp format (no timezone suffix)
	// This mirrors the actual data returned by indexd
	records := []OutputInfo{
		{
			Did:         "12286903-c5fd-5362-aed5-93bf7b8e8fff",
			CreatedDate: "2025-11-20T15:53:55.414023", // middle
			Authz:       []string{"/programs/cbds/projects/git_drs_test"},
		},
		{
			Did:         "65ab51a9-8fab-52d3-9be0-157d843108d1",
			CreatedDate: "2025-11-20T15:53:54.001713", // oldest - should be selected
			Authz:       []string{"/programs/cbds/projects/git_drs_test"},
		},
		{
			Did:         "f6be8bf7-d342-538c-8dc6-12721206e882",
			CreatedDate: "2025-11-20T15:53:56.792891", // newest
			Authz:       []string{"/programs/cbds/projects/git_drs_test"},
		},
	}

	canonical, err := FindEarliestCreatedRecordForProject(records, "cbds-git_drs_test")
	require.NoError(t, err)
	require.NotNil(t, canonical, "Should find a canonical record")
	require.Equal(t, "65ab51a9-8fab-52d3-9be0-157d843108d1", canonical.Did,
		"Should select the record with the earliest CreatedDate")
}
