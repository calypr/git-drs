package bucketselection

import (
	"slices"

	bucketapi "github.com/calypr/syfon/apigen/bucketapi"
	syfoncommon "github.com/calypr/syfon/client/access"
)

// MatchesResource returns the visible bucket names whose program claims match
// resource. Results are sorted and contain no duplicate bucket names.
func MatchesResource(payload bucketapi.BucketsResponse, resource string) []string {
	resource = syfoncommon.NormalizeAccessResource(resource)
	if resource == "" {
		return nil
	}

	matches := make([]string, 0)
	for bucket, meta := range payload.S3BUCKETS {
		if meta.Programs == nil {
			continue
		}
		for _, candidate := range *meta.Programs {
			if syfoncommon.NormalizeAccessResource(candidate) == resource {
				matches = append(matches, bucket)
				break
			}
		}
	}

	slices.Sort(matches)
	return slices.Compact(matches)
}
