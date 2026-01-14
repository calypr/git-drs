package s3_utils

import (
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/calypr/git-drs/drslog"
)

func TestCustomEndpointResolver(t *testing.T) {
	resolver := &CustomEndpointResolver{Endpoint: "https://s3.example.com"}
	endpoint, err := resolver.ResolveEndpoint("s3", "us-east-1")
	if err != nil {
		t.Fatalf("ResolveEndpoint error: %v", err)
	}
	if endpoint.URL != "https://s3.example.com" {
		t.Fatalf("unexpected endpoint: %s", endpoint.URL)
	}
	if endpoint.Source == aws.EndpointSourceCustom {
		// allow default value
	}
}

func TestAddURLOptions(t *testing.T) {
	cfg := &AddURLConfig{}

	s3Client := &s3.Client{}
	httpClient := &http.Client{}
	logger := drslog.NewNoOpLogger()

	WithS3Client(s3Client)(cfg)
	WithHTTPClient(httpClient)(cfg)
	WithLogger(logger)(cfg)

	if cfg.S3Client != s3Client || cfg.HttpClient != httpClient || cfg.Logger != logger {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
