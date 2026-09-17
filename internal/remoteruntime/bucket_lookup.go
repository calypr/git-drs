package remoteruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/bucketselection"
	"github.com/calypr/git-drs/internal/gitrepo"
	bucketapi "github.com/calypr/syfon/apigen/bucketapi"
	syclient "github.com/calypr/syfon/client"
	syfoncommon "github.com/calypr/syfon/client/access"
)

// resolveBucketScopeFromServer discovers the bucket exposed for a Gen3 scope.
// This is the runtime fallback for remotes configured with a scope but without
// an explicit storage location or repository-local bucket mapping.
func resolveBucketScopeFromServer(ctx context.Context, client *syclient.Client, organization, project, preferredBucket string) (gitrepo.ResolvedBucketScope, error) {
	if client == nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("bucket list client is required")
	}
	payload, err := client.Buckets().List(ctx)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("request bucket list: %w", err)
	}

	bucket, err := resolveBucketFromPayload(payload, organization, project, preferredBucket)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, err
	}
	return gitrepo.ResolvedBucketScope{Bucket: bucket}, nil
}

func resolveBucketFromPayload(payload bucketapi.BucketsResponse, organization, project, preferredBucket string) (string, error) {
	projectResource, err := syfoncommon.ResourcePath(organization, project)
	if err != nil {
		return "", err
	}
	orgResource, err := syfoncommon.ResourcePath(organization, "")
	if err != nil {
		return "", err
	}

	for _, resource := range []string{projectResource, orgResource} {
		matches := bucketselection.MatchesResource(payload, resource)
		if len(matches) == 0 {
			continue
		}
		if preferredBucket != "" {
			for _, bucket := range matches {
				if strings.EqualFold(bucket, strings.TrimSpace(preferredBucket)) {
					return bucket, nil
				}
			}
			return "", fmt.Errorf("selected bucket %q does not match resource %q; choose one of: %s", preferredBucket, resource, strings.Join(matches, ", "))
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		return "", fmt.Errorf("multiple visible server buckets matched resource %q: %s", resource, strings.Join(matches, ", "))
	}

	return "", fmt.Errorf("no visible server bucket matched organization=%q project=%q", organization, project)
}
