package cloud

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

func TestParseS3URL(t *testing.T) {
	bucket, key, err := ParseS3URL("s3://my-bucket/path/to/file.txt")
	if err != nil {
		t.Fatalf("ParseS3URL error: %v", err)
	}
	if bucket != "my-bucket" || key != "path/to/file.txt" {
		t.Fatalf("unexpected bucket/key: %s/%s", bucket, key)
	}
}

func TestParseS3URLErrors(t *testing.T) {
	t.Run("missing prefix", func(t *testing.T) {
		if _, _, err := ParseS3URL("http://bucket/key"); err == nil {
			t.Fatalf("expected error for missing s3 prefix")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		if _, _, err := ParseS3URL("s3://bucket"); err == nil {
			t.Fatalf("expected error for missing key")
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		if _, _, err := ParseS3URL("s3://bucket/"); err == nil {
			t.Fatalf("expected error for trailing slash")
		}
	})
}
