package client

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/utils"
)

type S3BucketsResponse struct {
	GSBuckets map[string]interface{} `json:"GS_BUCKETS"`
	S3Buckets map[string]S3Bucket    `json:"S3_BUCKETS"`
}

type S3Bucket struct {
	Region      string   `json:"region,omitempty"`
	EndpointURL string   `json:"endpoint_url,omitempty"`
	Programs    []string `json:"programs,omitempty"`
}

// S3Meta holds S3 object metadata
type S3Meta struct {
	Size         int64
	LastModified string
}

type customEndpointResolver struct {
	endpoint string
}

const (
	ADDURL_HELP_MSG            = "See git-drs add-url --help for more details."
	AWS_KEY_FLAG_NAME          = "aws-access-key-id"
	AWS_SECRET_FLAG_NAME       = "aws-secret-access-key"
	AWS_KEY_ENV_VAR            = "AWS_ACCESS_KEY_ID"
	AWS_SECRET_ENV_VAR         = "AWS_SECRET_ACCESS_KEY"
	AWS_REGION_FLAG_NAME       = "region"
	AWS_REGION_ENV_VAR         = "AWS_REGION"
	AWS_ENDPOINT_URL_FLAG_NAME = "endpoint-url"
	AWS_ENDPOINT_URL_ENV_VAR   = "AWS_ENDPOINT_URL"
)

func (r *customEndpointResolver) ResolveEndpoint(service, region string) (aws.Endpoint, error) {
	return aws.Endpoint{
		URL: r.endpoint,
	}, nil
}

// AddURLConfig holds optional clients for dependency injection
type AddURLConfig struct {
	s3Client   *s3.Client
	httpClient *http.Client
	logger     LoggerInterface
}

// AddURLOption is a functional option for configuring AddURL
type AddURLOption func(*AddURLConfig)

// WithS3Client provides a custom S3 client to AddURL
func WithS3Client(client *s3.Client) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.s3Client = client
	}
}

// WithHTTPClient provides a custom HTTP client to AddURL
func WithHTTPClient(client *http.Client) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.httpClient = client
	}
}

// WithLogger provides a custom logger to AddURL
func WithLogger(logger LoggerInterface) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.logger = logger
	}
}

// getBucketDetailsWithAuth fetches bucket details from Gen3 using an AuthHandler.
// This function accepts an auth handler for dependency injection, making it testable.
// Parameters:
//   - ctx: context for the request
//   - bucket: the bucket name to look up
//   - bucketsEndpointURL: full URL to the /user/data/buckets endpoint
//   - profile: the Gen3 profile to use for authentication
//   - authHandler: handler for adding authentication headers
//   - httpClient: the HTTP client to use
func getBucketDetailsWithAuth(ctx context.Context, bucket, bucketsEndpointURL, profile string, authHandler AuthHandler, httpClient *http.Client) (S3Bucket, error) {
	// Use provided client or create default
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", bucketsEndpointURL, nil)
	if err != nil {
		return S3Bucket{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication using the auth handler
	if authHandler != nil {
		if err := authHandler.AddAuthHeader(req, profile); err != nil {
			return S3Bucket{}, fmt.Errorf("failed to add authentication: %w", err)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return S3Bucket{}, fmt.Errorf("failed to fetch bucket information: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return S3Bucket{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// extract bucket endpoint
	var bucketInfo S3BucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&bucketInfo); err != nil {
		return S3Bucket{}, fmt.Errorf("failed to decode bucket information: %w", err)
	}

	if info, exists := bucketInfo.S3Buckets[bucket]; exists {
		if info.EndpointURL != "" && info.Region != "" {
			return info, nil
		}
		return S3Bucket{}, errors.New("endpoint_url or region not found for bucket")
	}

	return S3Bucket{}, nil
}

// getBucketDetails fetches bucket details from Gen3, loading config and auth.
// This is the production version that includes all config/auth dependencies.
func getBucketDetails(ctx context.Context, bucket string, httpClient *http.Client) (S3Bucket, error) {
	// load config
	cfg, err := config.LoadConfig()
	if err != nil {
		return S3Bucket{}, fmt.Errorf("failed to load config: %w", err)
	}

	// confirm current server exists and is gen3
	if cfg.CurrentServer != "gen3" && (cfg.Servers.Gen3 == nil || cfg.Servers.Gen3.Endpoint == "") {
		return S3Bucket{}, errors.New("Gen3 server endpoint is not configured in the config. Use `git drs list-config` to see and `git drs init` to .")
	}

	// get all buckets
	baseURL, err := url.Parse(cfg.Servers.Gen3.Endpoint)
	if err != nil {
		return S3Bucket{}, fmt.Errorf("failed to parse base URL: %w", err)
	}
	baseURL.Path = filepath.Join(baseURL.Path, "user/data/buckets")

	// Use the AuthHandler pattern for cleaner auth handling
	return getBucketDetailsWithAuth(ctx, bucket, baseURL.String(), cfg.Servers.Gen3.Auth.Profile, &RealAuthHandler{}, httpClient)
}

// fetchS3MetadataWithBucketDetails fetches S3 metadata given bucket details.
// This is the core testable logic, separated for easier unit testing.
func fetchS3MetadataWithBucketDetails(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string, bucketDetails S3Bucket, s3Client *s3.Client, logger LoggerInterface) (int64, string, error) {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &NoOpLogger{}
	}

	// Parse S3 URL
	bucket, key, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	// region + endpoint must be supplied if bucket not registered in gen3
	if bucketDetails.EndpointURL == "" || bucketDetails.Region == "" {
		logger.Log("Bucket details not found in Gen3 configuration. Using endpoint and region provided by user in CLI or in AWS configuration files.")
	}

	// Create s3 client if not passed as param
	var finalRegion, finalEndpoint string
	var finalCfg aws.Config
	var clientWasProvided bool = (s3Client != nil)

	if s3Client == nil {
		// Always load base AWS configuration first
		cfg, err := awsConfig.LoadDefaultConfig(ctx)
		if err != nil {
			return 0, "", fmt.Errorf("unable to load base AWS SDK config: %v. %s", err, ADDURL_HELP_MSG)
		}

		// Build config options to override defaults
		var configOptions []func(*awsConfig.LoadOptions) error

		// Override credentials if provided
		if awsAccessKey != "" && awsSecretKey != "" {
			configOptions = append(configOptions,
				awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
					awsAccessKey,
					awsSecretKey,
					"", // session token (empty for basic credentials)
				)),
			)
		}

		// Override region with priority: parameter > bucketDetails > default
		regionToUse := ""
		if region != "" {
			regionToUse = region
		} else if bucketDetails.Region != "" {
			regionToUse = bucketDetails.Region
		}
		if regionToUse != "" {
			configOptions = append(configOptions, awsConfig.WithRegion(regionToUse))
		}

		// Reload config with overrides if any options were set
		if len(configOptions) > 0 {
			cfg, err = awsConfig.LoadDefaultConfig(ctx, configOptions...)
			if err != nil {
				return 0, "", fmt.Errorf("unable to load AWS SDK config with overrides: %v. %s", err, ADDURL_HELP_MSG)
			}
		}

		// Determine endpoint with priority: parameter > bucketDetails > default config
		endpointToUse := ""
		if endpoint != "" {
			endpointToUse = endpoint
		} else if bucketDetails.EndpointURL != "" {
			endpointToUse = bucketDetails.EndpointURL
		}
		// Note: endpoint may also come from AWS config file, which will be loaded automatically

		// Store final values for validation
		finalRegion = cfg.Region
		finalCfg = cfg

		// Create S3 client with optional endpoint override and path-style addressing
		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			if endpointToUse != "" {
				o.BaseEndpoint = aws.String(endpointToUse)
			}
			o.UsePathStyle = true // This forces path-style URLs
		})
	}

	// Validate that all required configuration is present before making the HeadObject call
	// Only validate if we created the client ourselves (not provided as parameter)
	if !clientWasProvided {
		var missingFields []string

		// Check credentials
		if finalCfg.Credentials != nil {
			creds, err := finalCfg.Credentials.Retrieve(ctx)
			if err != nil || creds.AccessKeyID == "" {
				missingFields = append(missingFields, "AWS credentials (access key and secret key)")
			}
		}

		// Check region
		if finalRegion == "" {
			missingFields = append(missingFields, "AWS region")
		}

		// Check endpoint, ok if missing
		if finalEndpoint == "" {
			logger.Log("Warning: S3 endpoint URL is not provided. If supplied, using default AWS endpoint in configuration.")
		}

		// Note: We don't validate endpoint here because:
		// 1. It may be configured in AWS config file (which we can't easily inspect)
		// 2. For standard AWS S3, the endpoint is optional and determined by region

		// If any required fields are missing, return a clear error
		if len(missingFields) > 0 {
			var errorMsg strings.Builder
			errorMsg.WriteString("Missing required AWS configuration:\n")
			for i, field := range missingFields {
				errorMsg.WriteString(fmt.Sprintf("  %d. %s\n", i+1, field))
			}
			errorMsg.WriteString("\nPlease provide these values via:\n")
			errorMsg.WriteString("  - Command-line flags (--" + AWS_KEY_FLAG_NAME + ", --" + AWS_SECRET_FLAG_NAME + ", --" + AWS_REGION_FLAG_NAME + ", --" + AWS_ENDPOINT_URL_FLAG_NAME + ")\n")
			errorMsg.WriteString("  - Environment variables (" + AWS_KEY_ENV_VAR + ", " + AWS_SECRET_ENV_VAR + ", " + AWS_REGION_ENV_VAR + ", " + AWS_ENDPOINT_URL_ENV_VAR + ")\n")
			errorMsg.WriteString("  - AWS credentials file (~/.aws/credentials)\n")
			errorMsg.WriteString("  - Gen3 bucket registration (if bucket can be registered in Gen3)\n")
			errorMsg.WriteString("\n")
			errorMsg.WriteString(ADDURL_HELP_MSG)
			return 0, "", errors.New(errorMsg.String())
		}
	}

	// Ensure client was initialized (safety check)
	if s3Client == nil {
		return 0, "", fmt.Errorf("S3 client was not initialized. %s", ADDURL_HELP_MSG)
	}

	input := &s3.HeadObjectInput{
		Bucket: &bucket,
		Key:    aws.String(key),
	}

	resp, err := s3Client.HeadObject(ctx, input)
	if err != nil {
		return 0, "", fmt.Errorf("failed to head object, %v", err)
	}

	var contentLength int64
	if resp.ContentLength != nil {
		contentLength = *resp.ContentLength
	} else {
		contentLength = 0
	}

	return contentLength, resp.LastModified.Format(time.RFC3339), nil
}

// fetchS3Metadata fetches S3 metadata (size, modified date) for a given S3 URL.
// This is the production version that fetches bucket details from Gen3.
func fetchS3Metadata(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string, s3Client *s3.Client, httpClient *http.Client, logger LoggerInterface) (int64, string, error) {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &NoOpLogger{}
	}

	// Fetch AWS bucket region and endpoint from /data/buckets (fence in gen3)
	bucket, _, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	bucketDetails, err := getBucketDetails(ctx, bucket, httpClient)
	if err != nil {
		return 0, "", fmt.Errorf("Unable to get bucket details: %w. Please provide the AWS region and AWS bucket endpoint URL via flags or environment variables. %s", err, ADDURL_HELP_MSG)
	}

	return fetchS3MetadataWithBucketDetails(ctx, s3URL, awsAccessKey, awsSecretKey, region, endpoint, bucketDetails, s3Client, logger)
}

// upserts index record, so that if...
// 1. the record exists for the project, it updates the URL
// 2. the record for the project does not exist, it creates a new one
// upsertIndexdRecordWithClient is the core logic for upserting an indexd record.
// It's separated for easier unit testing with mock clients.
// Parameters:
//   - indexdClient: the indexd client interface (can be mocked)
//   - projectId: the project ID to use for the record
//   - url: the S3 URL to register
//   - sha256: the SHA256 hash of the file
//   - fileSize: the size of the file in bytes
//   - modifiedDate: the modification date of the file
//   - logger: the logger interface for output
func upsertIndexdRecordWithClient(indexdClient ObjectStoreClient, projectId, url, sha256 string, fileSize int64, modifiedDate string, logger LoggerInterface) error {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &NoOpLogger{}
	}

	// Extract relative path from S3 URL for UUID computation
	_, relPath, err := utils.ParseS3URL(url)
	if err != nil {
		return fmt.Errorf("failed to get relative S3 path from URL: %s", url)
	}

	// Compute deterministic UUID based on path and hash
	uuid := ComputeDeterministicUUID(relPath, sha256)

	// handle if record already exists
	records, err := indexdClient.GetObjectsByHash(string(drs.ChecksumTypeSHA256), sha256)
	if err != nil {
		return fmt.Errorf("Error querying indexd server for matches to hash %s: %v", sha256, err)
	}

	matchingRecord, err := FindMatchingRecord(records, projectId)
	if err != nil {
		return fmt.Errorf("Error finding matching record for project %s: %v", projectId, err)
	}

	if matchingRecord != nil && matchingRecord.Did == uuid {
		// if record exists and contains requested url, nothing to do
		if slices.Contains(matchingRecord.URLs, url) {
			logger.Log("Nothing to do: file already registered")
			return nil
		}

		// if record exists with different url, update via index/{guid}
		if matchingRecord.Did == uuid && !slices.Contains(matchingRecord.URLs, url) {
			logger.Log("updating existing record with new url")

			updateInfo := UpdateInputInfo{
				URLs: []string{url},
			}

			_, err := indexdClient.UpdateIndexdRecord(&updateInfo, matchingRecord.Did)
			if err != nil {
				return fmt.Errorf("failed to update indexd record: %w", err)
			}
			return nil
		}
	}

	// If no record exists, create indexd record
	logger.Log("creating new record")
	authzStr, err := utils.ProjectToResource(projectId)
	if err != nil {
		return err
	}

	indexdObject := &IndexdRecord{
		Did:      uuid,
		FileName: relPath,
		Hashes:   HashInfo{SHA256: sha256},
		Size:     fileSize,
		URLs:     []string{url},
		Authz:    []string{authzStr},
		Metadata: map[string]string{"remote": "true"},
		// ContentCreatedDate: modifiedDate, // TODO: setting created/updated time in indexd requires second API call
	}

	// save to local path similar to precommit hook
	// lets us skip over the add-url files during commit time without pinging server
	drsObjPath, err := GetObjectPath(config.DRS_OBJS_PATH, sha256, relPath)
	if err != nil {
		return fmt.Errorf("error getting DRS object path for oid %s: %v", sha256, err)
	}
	err = writeDrsObj(*indexdObject, sha256, drsObjPath)
	if err != nil {
		return fmt.Errorf("error writing DRS object for oid %s: %v", sha256, err)
	}

	_, err = indexdClient.RegisterIndexdRecord(indexdObject)
	if err != nil {
		return fmt.Errorf("failed to register indexd record: %w", err)
	}
	return nil
}

// upsertIndexdRecord is the production wrapper that loads config and creates clients.
func upsertIndexdRecord(url string, sha256 string, fileSize int64, modifiedDate string, logger LoggerInterface) error {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &NoOpLogger{}
	}

	indexdClient, err := NewIndexDClient(&NoOpLogger{})
	if err != nil {
		return fmt.Errorf("failed to initialize IndexD client: %w", err)
	}

	// get project ID
	projectId, err := config.GetProjectId()
	if err != nil {
		return fmt.Errorf("Error getting project ID: %v", err)
	}

	return upsertIndexdRecordWithClient(indexdClient, projectId, url, sha256, fileSize, modifiedDate, logger)
}

// AddURL adds a file to the Git DRS repo using an S3 URL
func AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...AddURLOption) (S3Meta, error) {
	// Create context with 10-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Apply options
	cfg := &AddURLConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Use NoOpLogger if no logger provided
	if cfg.logger == nil {
		cfg.logger = &NoOpLogger{}
	}

	// Validate inputs
	if err := validateInputs(s3URL, sha256); err != nil {
		return S3Meta{}, err
	}

	// check that lfs is tracking the file
	_, relPath, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return S3Meta{}, fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	// open .gitattributes
	isLFS, err := utils.IsLFSTracked(".gitattributes", relPath)
	if err != nil {
		return S3Meta{}, fmt.Errorf("unable to determine if file is tracked by LFS: %w", err)
	}
	if !isLFS {
		return S3Meta{}, fmt.Errorf("file is not tracked by LFS. Please run `git lfs track %s && git add .gitattributes` before proceeding", relPath)
	}

	// Fetch S3 metadata (size, modified date)
	cfg.logger.Log("Fetching S3 metadata...")
	fileSize, modifiedDate, err := fetchS3Metadata(ctx, s3URL, awsAccessKey, awsSecretKey, regionFlag, endpointFlag, cfg.s3Client, cfg.httpClient, cfg.logger)
	if err != nil {
		// if err contains 403, probably misconfigured credentials
		if strings.Contains(err.Error(), "403") {
			return S3Meta{}, fmt.Errorf("failed to fetch S3 metadata: %w. Double check your configured AWS credentials and endpoint url", err)
		}
		return S3Meta{}, fmt.Errorf("failed to fetch S3 metadata: %w", err)
	}
	cfg.logger.Log("Fetched S3 metadata successfully:")
	cfg.logger.Logf(" - File Size: %d bytes", fileSize)
	cfg.logger.Logf(" - Last Modified: %s", modifiedDate)

	// Create indexd record
	cfg.logger.Log("Processing indexd record...")
	if err := upsertIndexdRecord(s3URL, sha256, fileSize, modifiedDate, cfg.logger); err != nil {
		return S3Meta{}, fmt.Errorf("failed to create indexd record: %w", err)
	}
	cfg.logger.Log("Indexd updated")

	return S3Meta{
		Size:         fileSize,
		LastModified: modifiedDate,
	}, nil
}

func validateInputs(s3URL string, sha256 string) error {
	if !strings.HasPrefix(s3URL, "s3://") {
		return errors.New("invalid S3 URL format. URL should be of the format 's3://bucket/path/to/file'")
	}

	// Normalize case and validate SHA256
	sha256 = strings.ToLower(sha256)
	if len(sha256) != 64 {
		return errors.New("invalid SHA256 hash. Ensure it is a valid 64-character hexadecimal string.")
	}

	if _, err := hex.DecodeString(sha256); err != nil {
		return errors.New("invalid SHA256 hash. Ensure it is a valid 64-character hexadecimal string.")
	}

	return nil
}
