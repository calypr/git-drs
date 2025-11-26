package s3_utils

import (
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/git-drs/log"
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

// AuthHandler is an interface for adding authentication headers
// This allows us to inject different auth implementations for testing vs production
type AuthHandler interface {
	AddAuthHeader(req *http.Request, profile string) error
}

func (r *CustomEndpointResolver) ResolveEndpoint(service, region string) (aws.Endpoint, error) {
	return aws.Endpoint{
		URL: r.Endpoint,
	}, nil
}

// AddURLConfig holds optional clients for dependency injection
type AddURLConfig struct {
	S3Client   *s3.Client
	HttpClient *http.Client
	Logger     log.LoggerInterface
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
func WithLogger(logger log.LoggerInterface) AddURLOption {
	return func(cfg *AddURLConfig) {
		cfg.Logger = logger
	}
}
