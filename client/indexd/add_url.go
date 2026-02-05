package indexd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/fence"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/indexd"
	"github.com/calypr/data-client/s3utils"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/messages"
)

// getBucketDetails fetches bucket details from Gen3 using data-client.
func (inc *GitDrsIdxdClient) getBucketDetails(ctx context.Context, bucket string, httpClient *http.Client) (*fence.S3Bucket, error) {
	return inc.G3.Fence().GetBucketDetails(ctx, bucket)
}

// FetchS3MetadataWithBucketDetails fetches S3 metadata given bucket details.
func FetchS3MetadataWithBucketDetails(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string, bucketDetails *fence.S3Bucket, s3Client *s3.Client, logger *slog.Logger) (int64, string, error) {
	bucket, key, err := cloud.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	if s3Client == nil {
		cfg, err := awsConfig.LoadDefaultConfig(ctx)
		if err != nil {
			return 0, "", fmt.Errorf("unable to load base AWS SDK config: %v. %s", err, messages.ADDURL_HELP_MSG)
		}

		var configOptions []func(*awsConfig.LoadOptions) error
		if awsAccessKey != "" && awsSecretKey != "" {
			configOptions = append(configOptions,
				awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(awsAccessKey, awsSecretKey, "")),
			)
		}

		regionToUse := ""
		if region != "" {
			regionToUse = region
		} else if bucketDetails != nil && bucketDetails.Region != "" {
			regionToUse = bucketDetails.Region
		}
		if regionToUse != "" {
			configOptions = append(configOptions, awsConfig.WithRegion(regionToUse))
		}

		if len(configOptions) > 0 {
			cfg, err = awsConfig.LoadDefaultConfig(ctx, configOptions...)
			if err != nil {
				return 0, "", fmt.Errorf("unable to load AWS SDK config with overrides: %v. %s", err, messages.ADDURL_HELP_MSG)
			}
		}

		endpointToUse := ""
		if endpoint != "" {
			endpointToUse = endpoint
		} else if bucketDetails != nil && bucketDetails.EndpointURL != "" {
			endpointToUse = bucketDetails.EndpointURL
		}

		s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			if endpointToUse != "" {
				o.BaseEndpoint = aws.String(endpointToUse)
			}
			o.UsePathStyle = true
		})
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
	}

	return contentLength, resp.LastModified.Format(time.RFC3339), nil
}

func (inc *GitDrsIdxdClient) fetchS3Metadata(ctx context.Context, s3URL, awsAccessKey, awsSecretKey, region, endpoint string, s3Client *s3.Client, httpClient *http.Client, logger *slog.Logger) (int64, string, error) {
	bucket, _, err := cloud.ParseS3URL(s3URL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	bucketDetails, err := inc.getBucketDetails(ctx, bucket, httpClient)
	if err != nil {
		logger.Debug(fmt.Sprintf("Warning: unable to get bucket details from Gen3: %v", err))
	}

	return FetchS3MetadataWithBucketDetails(ctx, s3URL, awsAccessKey, awsSecretKey, region, endpoint, bucketDetails, s3Client, logger)
}

func (inc *GitDrsIdxdClient) upsertIndexdRecord(ctx context.Context, url string, sha256 string, fileSize int64, logger *slog.Logger) (*drs.DRSObject, error) {
	projectId := inc.GetProjectId()
	uuid := drsmap.DrsUUID(projectId, sha256)

	records, err := inc.GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: sha256})
	if err != nil {
		return nil, fmt.Errorf("error querying indexd server: %v", err)
	}

	var matchingRecord *drs.DRSObject
	for i := range records {
		if records[i].Id == uuid {
			matchingRecord = &records[i]
			break
		}
	}

	if matchingRecord != nil {
		existingURLs := indexd.IndexdURLFromDrsAccessURLs(matchingRecord.AccessMethods)
		if slices.Contains(existingURLs, url) {
			logger.Debug("Nothing to do: file already registered")
			return matchingRecord, nil
		}

		logger.Debug("updating existing record with new url")
		updatedRecord := drs.DRSObject{AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: url}}}}
		return inc.UpdateRecord(ctx, &updatedRecord, matchingRecord.Id)
	}

	// If no record exists, create one
	logger.Debug("creating new record")
	_, relPath, _ := cloud.ParseS3URL(url)

	drsObj, err := drs.BuildDrsObj(relPath, sha256, fileSize, uuid, inc.Config.BucketName, projectId)
	if err != nil {
		return nil, err
	}

	// Add authz explicitly since BuildDrsObj might not set it exactly as needed for all cases
	// Actually BuildDrsObj does set authz.
	return inc.RegisterRecord(ctx, drsObj)
}

func (inc *GitDrsIdxdClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...cloud.AddURLOption) (s3utils.S3Meta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := &cloud.AddURLConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if inc.Logger == nil {
		inc.Logger = drslog.NewNoOpLogger()
	}

	if err := s3utils.ValidateInputs(s3URL, sha256); err != nil {
		return s3utils.S3Meta{}, err
	}

	_, relPath, err := cloud.ParseS3URL(s3URL)
	if err != nil {
		return s3utils.S3Meta{}, fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	isLFS, err := lfs.IsLFSTracked(relPath)
	if err != nil {
		return s3utils.S3Meta{}, fmt.Errorf("unable to determine if file is tracked by LFS: %w", err)
	}
	if !isLFS {
		return s3utils.S3Meta{}, fmt.Errorf("file is not tracked by LFS")
	}

	inc.Logger.Debug("Fetching S3 metadata...")
	fileSize, modifiedDate, err := inc.fetchS3Metadata(ctx, s3URL, awsAccessKey, awsSecretKey, regionFlag, endpointFlag, cfg.S3Client, cfg.HttpClient, inc.Logger)
	if err != nil {
		return s3utils.S3Meta{}, fmt.Errorf("failed to fetch S3 metadata: %w", err)
	}

	inc.Logger.Debug(fmt.Sprintf("Fetched S3 metadata successfully: %d bytes, modified: %s", fileSize, modifiedDate))

	inc.Logger.Debug("Processing indexd record...")
	drsObj, err := inc.upsertIndexdRecord(ctx, s3URL, sha256, fileSize, inc.Logger)
	if err != nil {
		return s3utils.S3Meta{}, fmt.Errorf("failed to create indexd record: %w", err)
	}

	drsObjPath, err := drsmap.GetObjectPath(common.DRS_OBJS_PATH, drsObj.Checksums.SHA256)
	if err != nil {
		return s3utils.S3Meta{}, err
	}
	if err := drsmap.WriteDrsObj(drsObj, sha256, drsObjPath); err != nil {
		return s3utils.S3Meta{}, err
	}

	inc.Logger.Debug("Indexd updated")

	return s3utils.S3Meta{
		Size:         fileSize,
		LastModified: modifiedDate,
	}, nil
}
