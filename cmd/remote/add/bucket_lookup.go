package add

import (
	"fmt"
	"slices"
	"strings"

	bucketapi "github.com/calypr/syfon/apigen/client/bucketapi"
	syfoncommon "github.com/calypr/syfon/common"
)

func resolveBucketFromPayload(payload bucketapi.BucketsResponse, organization, project, preferredBucket string) (string, error) {
	projectResource, err := syfoncommon.ResourcePath(organization, project)
	if err != nil {
		return "", err
	}
	orgResource, err := syfoncommon.ResourcePath(organization, "")
	if err != nil {
		return "", err
	}

	if bucket, err := chooseVisibleBucket(payload, projectResource, preferredBucket); err != nil || bucket != "" {
		return bucket, err
	}
	if bucket, err := chooseVisibleBucket(payload, orgResource, preferredBucket); err != nil || bucket != "" {
		return bucket, err
	}

	return "", fmt.Errorf("no visible server bucket matched organization=%q project=%q", organization, project)
}

func chooseVisibleBucket(payload bucketapi.BucketsResponse, resource, preferredBucket string) (string, error) {
	matches := findBucketsByResource(payload, resource)
	if len(matches) == 0 {
		return "", nil
	}

	preferredBucket = strings.TrimSpace(preferredBucket)
	if preferredBucket != "" {
		for _, bucket := range matches {
			if strings.EqualFold(bucket, preferredBucket) {
				return bucket, nil
			}
		}
		return "", fmt.Errorf("selected bucket %q does not match resource %q; choose one of: %s", preferredBucket, resource, strings.Join(matches, ", "))
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", fmt.Errorf("multiple visible server buckets matched resource %q: %s; rerun with --bucket <bucket>", resource, strings.Join(matches, ", "))
}

func findBucketsByResource(payload bucketapi.BucketsResponse, resource string) []string {
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
