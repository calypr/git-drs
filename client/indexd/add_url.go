package indexd_client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/log"
	"github.com/calypr/git-drs/messages"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/calypr/git-drs/utils"
)

// getBucketDetails fetches bucket details from Gen3, loading config and auth.
// This is the production version that includes all config/auth dependencies.
func (inc *IndexDClient) getBucketDetails(ctx context.Context, bucket string, httpClient *http.Client) (s3_utils.S3Bucket, error) {
	// get all buckets
	baseURL := inc.Base
	baseURL.Path = filepath.Join(baseURL.Path, "user/data/buckets")
	// Use the AuthHandler pattern for cleaner auth handling
	return GetBucketDetailsWithAuth(ctx, bucket, baseURL.String(), inc.Profile, &RealAuthHandler{}, httpClient)
}

// FetchS3MetadataWithBucketDetails fetches S3 metadata given bucket details.
// This is the core testable logic, separated for easier unit testing.
func FetchS3MetadataWithBucketDetails(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string, bucketDetails s3_utils.S3Bucket, s3Client *s3.Client, logger log.LoggerInterface) (int64, string, error) {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &log.NoOpLogger{}
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
			return 0, "", fmt.Errorf("unable to load base AWS SDK config: %v. %s", err, messages.ADDURL_HELP_MSG)
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
				return 0, "", fmt.Errorf("unable to load AWS SDK config with overrides: %v. %s", err, messages.ADDURL_HELP_MSG)
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
			errorMsg.WriteString("  - Command-line flags (--" + s3_utils.AWS_KEY_FLAG_NAME + ", --" + s3_utils.AWS_SECRET_FLAG_NAME + ", --" + s3_utils.AWS_REGION_FLAG_NAME + ", --" + s3_utils.AWS_ENDPOINT_URL_FLAG_NAME + ")\n")
			errorMsg.WriteString("  - Environment variables (" + s3_utils.AWS_KEY_ENV_VAR + ", " + s3_utils.AWS_SECRET_ENV_VAR + ", " + s3_utils.AWS_REGION_ENV_VAR + ", " + s3_utils.AWS_ENDPOINT_URL_ENV_VAR + ")\n")
			errorMsg.WriteString("  - AWS credentials file (~/.aws/credentials)\n")
			errorMsg.WriteString("  - Gen3 bucket registration (if bucket can be registered in Gen3)\n")
			errorMsg.WriteString("\n")
			errorMsg.WriteString(messages.ADDURL_HELP_MSG)
			return 0, "", errors.New(errorMsg.String())
		}
	}

	// Ensure client was initialized (safety check)
	if s3Client == nil {
		return 0, "", fmt.Errorf("S3 client was not initialized. %s", messages.ADDURL_HELP_MSG)
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
func (inc *IndexDClient) fetchS3Metadata(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string, s3Client *s3.Client, httpClient *http.Client, logger log.LoggerInterface) (int64, string, error) {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &log.NoOpLogger{}
	}

	// Fetch AWS bucket region and endpoint from /data/buckets (fence in gen3)
	bucket, _, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	bucketDetails, err := inc.getBucketDetails(ctx, bucket, httpClient)
	if err != nil {
		return 0, "", fmt.Errorf("Unable to get bucket details: %w. Please provide the AWS region and AWS bucket endpoint URL via flags or environment variables. %s", err, messages.ADDURL_HELP_MSG)
	}

	return FetchS3MetadataWithBucketDetails(ctx, s3URL, awsAccessKey, awsSecretKey, region, endpoint, bucketDetails, s3Client, logger)
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
func upsertIndexdRecordWithClient(indexdClient client.DRSClient, projectId, url, sha256 string, fileSize int64, modifiedDate string, logger log.LoggerInterface) error {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &log.NoOpLogger{}
	}

	uuid := drsmap.DrsUUID(projectId, sha256)

	// handle if record already exists
	records, err := indexdClient.GetObjectsByHash(string(drs.ChecksumTypeSHA256), sha256)
	if err != nil {
		return fmt.Errorf("Error querying indexd server for matches to hash %s: %v", sha256, err)
	}

	matchingRecord, err := drsmap.FindMatchingRecord(records, projectId)
	if err != nil {
		return fmt.Errorf("Error finding matching record for project %s: %v", projectId, err)
	}

	if matchingRecord != nil && matchingRecord.Id == uuid {
		// if record exists and contains requested url, nothing to do
		if slices.Contains(indexdURLFromDrsAccessURLs(matchingRecord.AccessMethods), url) {
			logger.Log("Nothing to do: file already registered")
			return nil
		}

		// if record exists with different url, update via index/{guid}
		if matchingRecord.Id == uuid && !slices.Contains(indexdURLFromDrsAccessURLs(matchingRecord.AccessMethods), url) {
			logger.Log("updating existing record with new url")

			//updateInfo := UpdateInputInfo{
			//	URLs: []string{url},
			//}
			//TODO: this assumes that files aren't stored in multiple locations....
			updatedRecord := drs.DRSObject{AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: url}}}}

			_, err := indexdClient.UpdateRecord(&updatedRecord, matchingRecord.Id)
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
	_, relPath, err := utils.ParseS3URL(url)
	if err != nil {
		return fmt.Errorf("failed to get relative S3 path from URL: %s", url)
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

	_, err = indexdClient.RegisterRecord(indexdObject.ToDrsObject())
	if err != nil {
		return fmt.Errorf("failed to register indexd record: %w", err)
	}
	return nil
}

// upsertIndexdRecord is the production wrapper that loads config and creates clients.
func (inc *IndexDClient) upsertIndexdRecord(url string, sha256 string, fileSize int64, modifiedDate string, logger log.LoggerInterface) error {
	// Use NoOpLogger if no logger provided
	if logger == nil {
		logger = &log.NoOpLogger{}
	}
	return upsertIndexdRecordWithClient(inc, inc.ProjectId, url, sha256, fileSize, modifiedDate, logger)
}

// AddURL adds a file to the Git DRS repo using an S3 URL
func (inc *IndexDClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...s3_utils.AddURLOption) (s3_utils.S3Meta, error) {
	// Create context with 10-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Apply options
	cfg := &s3_utils.AddURLConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Use NoOpLogger if no logger provided
	if inc.Logger == nil {
		//TODO: re-enable logging
		//cfg.logger = &log.NoOpLogger{}
	}

	// Validate inputs
	if err := s3_utils.ValidateInputs(s3URL, sha256); err != nil {
		return s3_utils.S3Meta{}, err
	}

	// check that lfs is tracking the file
	_, relPath, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return s3_utils.S3Meta{}, fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	// open .gitattributes
	isLFS, err := utils.IsLFSTracked(".gitattributes", relPath)
	if err != nil {
		return s3_utils.S3Meta{}, fmt.Errorf("unable to determine if file is tracked by LFS: %w", err)
	}
	if !isLFS {
		return s3_utils.S3Meta{}, fmt.Errorf("file is not tracked by LFS. Please run `git lfs track %s && git add .gitattributes` before proceeding", relPath)
	}

	// Fetch S3 metadata (size, modified date)
	inc.Logger.Log("Fetching S3 metadata...")
	fileSize, modifiedDate, err := inc.fetchS3Metadata(ctx, s3URL, awsAccessKey, awsSecretKey, regionFlag, endpointFlag, cfg.S3Client, cfg.HttpClient, cfg.Logger)
	if err != nil {
		// if err contains 403, probably misconfigured credentials
		if strings.Contains(err.Error(), "403") {
			return s3_utils.S3Meta{}, fmt.Errorf("failed to fetch S3 metadata: %w. Double check your configured AWS credentials and endpoint url", err)
		}
		return s3_utils.S3Meta{}, fmt.Errorf("failed to fetch S3 metadata: %w", err)
	}
	inc.Logger.Log("Fetched S3 metadata successfully:")
	inc.Logger.Logf(" - File Size: %d bytes", fileSize)
	inc.Logger.Logf(" - Last Modified: %s", modifiedDate)

	// Create indexd record
	inc.Logger.Log("Processing indexd record...")
	if err := inc.upsertIndexdRecord(s3URL, sha256, fileSize, modifiedDate, inc.Logger); err != nil {
		return s3_utils.S3Meta{}, fmt.Errorf("failed to create indexd record: %w", err)
	}
	inc.Logger.Log("Indexd updated")

	return s3_utils.S3Meta{
		Size:         fileSize,
		LastModified: modifiedDate,
	}, nil
}
