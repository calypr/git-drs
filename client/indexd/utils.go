package indexd_client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/calypr/git-drs/s3_utils"
)

// getBucketDetailsWithAuth fetches bucket details from Gen3 using an AuthHandler.
// This function accepts an auth handler for dependency injection, making it testable.
// Parameters:
//   - ctx: context for the request
//   - bucket: the bucket name to look up
//   - bucketsEndpointURL: full URL to the /user/data/buckets endpoint
//   - profile: the Gen3 profile to use for authentication
//   - authHandler: handler for adding authentication headers
//   - httpClient: the HTTP client to use
func GetBucketDetailsWithAuth(ctx context.Context, bucket, bucketsEndpointURL, profile string, authHandler s3_utils.AuthHandler, httpClient *http.Client) (*s3_utils.S3Bucket, error) {
	// Use provided client or create default
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", bucketsEndpointURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication using the auth handler
	if authHandler != nil {
		if err := authHandler.AddAuthHeader(req, profile); err != nil {
			return nil, fmt.Errorf("failed to add authentication: %w", err)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch bucket information: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// extract bucket endpoint
	var bucketInfo s3_utils.S3BucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&bucketInfo); err != nil {
		return nil, fmt.Errorf("failed to decode bucket information: %w", err)
	}

	if info, exists := bucketInfo.S3Buckets[bucket]; exists {
		if info.EndpointURL != "" && info.Region != "" {
			return info, nil
		}
		return nil, errors.New("endpoint_url or region not found for bucket")
	}

	return nil, nil
}
