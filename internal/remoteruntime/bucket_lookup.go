package remoteruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
	bucketapi "github.com/calypr/syfon/apigen/client/bucketapi"
	syfoncommon "github.com/calypr/syfon/common"
)

// resolveBucketScopeFromServer discovers the bucket exposed for a Gen3 scope.
// This is the runtime fallback for remotes configured with a scope but without
// an explicit storage location or repository-local bucket mapping.
func resolveBucketScopeFromServer(ctx context.Context, endpoint, token, organization, project, preferredBucket string) (gitrepo.ResolvedBucketScope, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/data/buckets", nil)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("build bucket list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("request bucket list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("bucket list failed with status %d", resp.StatusCode)
	}

	var payload bucketapi.BucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("decode bucket list response: %w", err)
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
		matches := make([]string, 0)
		for bucket, meta := range payload.S3BUCKETS {
			if meta.Programs == nil {
				continue
			}
			for _, candidate := range *meta.Programs {
				if syfoncommon.NormalizeAccessResource(candidate) == syfoncommon.NormalizeAccessResource(resource) {
					matches = append(matches, bucket)
					break
				}
			}
		}
		slices.Sort(matches)
		matches = slices.Compact(matches)
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
