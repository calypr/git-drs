package indexd_client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	token "github.com/bmeg/grip-graphql/middleware"
	"github.com/calypr/data-client/client/commonUtils"
	"github.com/calypr/data-client/client/jwt"
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
func GetBucketDetailsWithAuth(ctx context.Context, bucket, bucketsEndpointURL, profile string, authHandler s3_utils.AuthHandler, httpClient *http.Client) (s3_utils.S3Bucket, error) {
	// Use provided client or create default
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", bucketsEndpointURL, nil)
	if err != nil {
		return s3_utils.S3Bucket{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication using the auth handler
	if authHandler != nil {
		if err := authHandler.AddAuthHeader(req, profile); err != nil {
			return s3_utils.S3Bucket{}, fmt.Errorf("failed to add authentication: %w", err)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return s3_utils.S3Bucket{}, fmt.Errorf("failed to fetch bucket information: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return s3_utils.S3Bucket{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// extract bucket endpoint
	var bucketInfo s3_utils.S3BucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&bucketInfo); err != nil {
		return s3_utils.S3Bucket{}, fmt.Errorf("failed to decode bucket information: %w", err)
	}

	if info, exists := bucketInfo.S3Buckets[bucket]; exists {
		if info.EndpointURL != "" && info.Region != "" {
			return info, nil
		}
		return s3_utils.S3Bucket{}, errors.New("endpoint_url or region not found for bucket")
	}

	return s3_utils.S3Bucket{}, nil
}

func addGen3AuthHeader(req *http.Request, profile string) error {
	// extract accessToken from gen3 profile and insert into header of request
	var err error
	if ProfileConfig.AccessToken == "" {
		ProfileConfig, err = conf.ParseConfig(profile)
		if err != nil {
			if errors.Is(err, jwt.ErrProfileNotFound) {
				return fmt.Errorf("Profile not in config file. Need to run 'git drs init' for gen3 first, see git drs init --help\n")
			}
			return fmt.Errorf("error parsing gen3 config: %s", err)
		}
	}
	if ProfileConfig.AccessToken == "" {
		return fmt.Errorf("access token not found in profile config")
	}
	expiration, err := token.GetExpiration(ProfileConfig.AccessToken)
	if err != nil {
		return err
	}
	// Update AccessToken if token is old
	if expiration.Before(time.Now()) {
		r := jwt.Request{}
		err = r.RequestNewAccessToken(ProfileConfig.APIEndpoint+commonUtils.FenceAccessTokenEndpoint, &ProfileConfig)
		if err != nil {
			// load config and see if the endpoint is printed
			errStr := fmt.Sprintf("error refreshing access token: %v", err)
			if strings.Contains(errStr, "no such host") {
				errStr += ". If you are accessing an internal website, make sure you are connected to the internal network."
			}

			return errors.New(errStr)
		}
	}

	// Add headers to the request
	authStr := "Bearer " + ProfileConfig.AccessToken
	req.Header.Set("Authorization", authStr)

	return nil
}
