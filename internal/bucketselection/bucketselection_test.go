package bucketselection

import (
	"testing"

	bucketapi "github.com/calypr/syfon/apigen/bucketapi"
)

func TestMatchesResourceSortsAndDeduplicatesBuckets(t *testing.T) {
	programs := []string{
		"/organization/org/project/project",
		"/organization/org/project/project/",
	}
	payload := bucketapi.BucketsResponse{S3BUCKETS: map[string]bucketapi.BucketMetadata{
		"zeta":  {Programs: &programs},
		"alpha": {Programs: &programs},
		"empty": {},
		"other": {Programs: stringSlice("/organization/other/project/project")},
	}}

	got := MatchesResource(payload, "/organization/org/project/project")
	want := []string{"alpha", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("MatchesResource returned %d buckets, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("MatchesResource[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMatchesResourceIgnoresEmptyAndInvalidResources(t *testing.T) {
	programs := []string{"/organization/org/project/project"}
	payload := bucketapi.BucketsResponse{S3BUCKETS: map[string]bucketapi.BucketMetadata{
		"bucket": {Programs: &programs},
	}}

	if got := MatchesResource(payload, ""); got != nil {
		t.Errorf("MatchesResource(empty resource) = %v, want nil", got)
	}
	for _, resource := range []string{"project/project", "/organization/other/project/project"} {
		if got := MatchesResource(payload, resource); len(got) != 0 {
			t.Errorf("MatchesResource(%q) = %v, want no matches", resource, got)
		}
	}
}

func stringSlice(values ...string) *[]string {
	return &values
}
