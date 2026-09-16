package add

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

func resolveBucketScopeFromServer(ctx context.Context, endpoint, token, organization, project, preferredBucket string) (gitrepo.ResolvedBucketScope, error) {
	if strings.TrimSpace(endpoint) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing API endpoint for server bucket lookup")
	}
	if strings.TrimSpace(token) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing access token for server bucket lookup")
	}

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

func resolveBucketScopeFromLocalServer(ctx context.Context, endpoint, username, password, organization, project, preferredBucket string) (gitrepo.ResolvedBucketScope, error) {
	if strings.TrimSpace(endpoint) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing API endpoint for server bucket lookup")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/data/buckets", nil)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("build bucket list request: %w", err)
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}

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
