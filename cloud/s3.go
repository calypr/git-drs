package cloud

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3BucketsResponse struct {
	GSBuckets map[string]any       `json:"GS_BUCKETS"`
	S3Buckets map[string]*S3Bucket `json:"S3_BUCKETS"`
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

type CustomEndpointResolver struct {
	Endpoint string
}

const (
	AWS_KEY_FLAG_NAME          = "aws-access-key-id"
	AWS_SECRET_FLAG_NAME       = "aws-secret-access-key"
	AWS_KEY_ENV_VAR            = "AWS_ACCESS_KEY_ID"
	AWS_SECRET_ENV_VAR         = "AWS_SECRET_ACCESS_KEY"
	AWS_REGION_FLAG_NAME       = "region"
	AWS_REGION_ENV_VAR         = "AWS_REGION"
	AWS_ENDPOINT_URL_FLAG_NAME = "endpoint-url"
	AWS_ENDPOINT_URL_ENV_VAR   = "AWS_ENDPOINT_URL"
)

func (r *CustomEndpointResolver) ResolveEndpoint(service, region string) (aws.Endpoint, error) {
	return aws.Endpoint{
		URL: r.Endpoint,
	}, nil
}

// AddURLConfig holds optional clients for dependency injection
type AddURLConfig struct {
	S3Client   *s3.Client
	HttpClient *http.Client
	Logger     *slog.Logger
}

// AddURLOption is a functional option for configuring AddURL
type AddURLOption func(*AddURLConfig)

// WithS3Client provides a custom S3 client to AddURL
func WithS3Client(client *s3.Client) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.S3Client = client
	}
}

// WithHTTPClient provides a custom HTTP client to AddURL
func WithHTTPClient(client *http.Client) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.HttpClient = client
	}
}

// WithLogger provides a custom logger to AddURL
func WithLogger(logger *slog.Logger) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.Logger = logger
	}
}

func ParseS3URL(s3url string) (string, string, error) {
	s3Prefix := "s3://"
	if !strings.HasPrefix(s3url, s3Prefix) {
		return "", "", fmt.Errorf("S3 URL requires prefix 's3://': %s", s3url)
	}
	trimmed := strings.TrimPrefix(s3url, s3Prefix)
	slashIndex := strings.Index(trimmed, "/")
	if slashIndex == -1 || slashIndex == len(trimmed)-1 {
		return "", "", fmt.Errorf("invalid S3 file URL: %s", s3url)
	}
	return trimmed[:slashIndex], trimmed[slashIndex+1:], nil
}

// CanDownloadFile checks if a file can be downloaded from the given signed URL
// by issuing a ranged GET for a single byte to mimic HEAD behavior.
func CanDownloadFile(signedURL string) error {
	req, err := http.NewRequest("GET", signedURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error while sending the request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	return fmt.Errorf("failed to access file, HTTP status: %d", resp.StatusCode)
}
