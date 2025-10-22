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

type customEndpointResolver struct {
	endpoint string
}

func (r *customEndpointResolver) ResolveEndpoint(service, region string) (aws.Endpoint, error) {
	return aws.Endpoint{
		URL: r.endpoint,
	}, nil
}

func getBucketDetails(ctx context.Context, bucket string) (S3Bucket, error) {
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
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL.String(), nil)
	if err != nil {
		return S3Bucket{}, fmt.Errorf("failed to create request: %w", err)
	}

	if err := addGen3AuthHeader(req, cfg.Servers.Gen3.Auth.Profile); err != nil {
		return S3Bucket{}, fmt.Errorf("failed to add Gen3 authentication: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
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

	return S3Bucket{}, errors.New("bucket not found")
}

func fetchS3Metadata(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string) (int64, string, error) {
	// Fetch bucket endpoint from /data/buckets
	bucket, key, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	bucketDetails, err := getBucketDetails(ctx, bucket)
	if err != nil {
		fmt.Println("Bucket details not found in Gen3 configuration. Using provided endpoint and region flags.")
		bucketDetails = S3Bucket{
			Region:      region,
			EndpointURL: endpoint,
		}
	}

	// Load AWS configuration
	var cfg aws.Config
	if awsAccessKey != "" && awsSecretKey != "" {
		cfg, err = awsConfig.LoadDefaultConfig(ctx,
			awsConfig.WithRegion(bucketDetails.Region),
			awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				awsAccessKey,
				awsSecretKey,
				"", // session token (empty for basic credentials)
			)),
			awsConfig.WithSharedConfigFiles([]string{}),      // Disable shared config file
			awsConfig.WithSharedCredentialsFiles([]string{}), // Disable shared credentials file
		)
		if err != nil {
			return 0, "", fmt.Errorf("unable to load SDK config with static credentials: %v", err)
		}
	}

	// Create S3 client with custom endpoint and path-style addressing
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(bucketDetails.EndpointURL)
		o.UsePathStyle = true // This forces path-style URLs
	})

	// print bucket details
	// fmt.Printf("Bucket Details: %+v\n", bucketDetails)

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

// upserts index record, so that if...
// 1. the record exists for the project, it updates the URL
// 2. the record for the project does not exist, it creates a new one
func upsertIndexdRecord(url string, sha256 string, fileSize int64, modifiedDate string) error {
	// setup indexd client
	logger, err := NewLogger("", false)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	indexdClient, err := NewIndexDClient(logger)
	if err != nil {
		return fmt.Errorf("failed to initialize IndexD client: %w", err)
	}

	// get project ID and UUID
	projectId, err := config.GetProjectId()
	if err != nil {
		return fmt.Errorf("Error getting project ID: %v", err)
	}

	uuid := DrsUUID(projectId, sha256)

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
			fmt.Println("Nothing to do: file already registered")
			return nil
		}

		// if record exists with different url, update via index/{guid}
		if matchingRecord.Did == uuid && !slices.Contains(matchingRecord.URLs, url) {
			fmt.Println("updating existing record with new url")

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

	_, err = indexdClient.RegisterIndexdRecord(indexdObject)
	if err != nil {
		return fmt.Errorf("failed to register indexd record: %w", err)
	}

	fmt.Println("Indexd record created successfully.")
	return nil
}

// AddURL adds a file to the Git DRS repo using an S3 URL
func AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string) (int64, string, error) {
	// Create context with 10-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Validate inputs
	if err := validateInputs(s3URL, sha256); err != nil {
		return 0, "", err
	}

	// check that lfs is tracking the file
	_, relPath, err := utils.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	// open .gitattributes
	isLFS, err := utils.IsLFSTracked(".gitattributes", relPath)
	if err != nil {
		return 0, "", fmt.Errorf("unable to determine if file is tracked by LFS: %w", err)
	}
	if !isLFS {
		return 0, "", fmt.Errorf("file is not tracked by LFS. Please run `git lfs track %s && git add .gitattributes` before proceeding", relPath)
	}

	// Fetch S3 metadata (size, modified date)
	fileSize, modifiedDate, err := fetchS3Metadata(ctx, s3URL, awsAccessKey, awsSecretKey, regionFlag, endpointFlag)
	if err != nil {
		return 0, "", fmt.Errorf("failed to fetch S3 metadata: %w", err)
	}
	fmt.Println("Fetched S3 metadata successfully:")
	fmt.Printf(" - File Size: %d bytes\n", fileSize)
	fmt.Printf(" - Last Modified: %s\n", modifiedDate)

	// Create indexd record
	if err := upsertIndexdRecord(s3URL, sha256, fileSize, modifiedDate); err != nil {
		return 0, "", fmt.Errorf("failed to create indexd record: %w", err)
	}

	return fileSize, modifiedDate, nil
}

func validateInputs(s3URL string, sha256 string) error {
	if !strings.HasPrefix(s3URL, "s3://") {
		return errors.New("invalid S3 URL format. Please ensure the URL starts with 's3://'")
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
