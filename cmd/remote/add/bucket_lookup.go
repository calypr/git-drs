package add

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/calypr/git-drs/internal/bucketselection"
	"github.com/calypr/git-drs/internal/gitrepo"
	bucketapi "github.com/calypr/syfon/apigen/bucketapi"
	syclient "github.com/calypr/syfon/client"
	syfoncommon "github.com/calypr/syfon/client/access"
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
	matches := bucketselection.MatchesResource(payload, resource)
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

func resolveBucketScopeFromServer(ctx context.Context, endpoint, token, organization, project, preferredBucket string) (gitrepo.ResolvedBucketScope, error) {
	if strings.TrimSpace(endpoint) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing API endpoint for server bucket lookup")
	}
	if strings.TrimSpace(token) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing access token for server bucket lookup")
	}

	client, err := syclient.New(endpoint,
		syclient.WithHTTPClient(http.DefaultClient),
		syclient.WithBearerToken(token),
	)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("build bucket list client: %w", err)
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

func resolveBucketScopeFromLocalServer(ctx context.Context, endpoint, username, password, organization, project, preferredBucket string) (gitrepo.ResolvedBucketScope, error) {
	if strings.TrimSpace(endpoint) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing API endpoint for server bucket lookup")
	}

	client, err := syclient.New(endpoint,
		syclient.WithHTTPClient(http.DefaultClient),
		syclient.WithBasicAuth(username, password),
	)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("build bucket list client: %w", err)
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
