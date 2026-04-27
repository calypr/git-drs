package cloud

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseObjectLocation_S3Scheme(t *testing.T) {
	loc, err := parseObjectLocation("s3://my-bucket/path/to/file.bam", "", ObjectParameters{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if loc.bucket != "my-bucket" {
		t.Fatalf("bucket mismatch: %q", loc.bucket)
	}
	if loc.key != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", loc.key)
	}
	if loc.bucketURL != "s3://my-bucket" {
		t.Fatalf("bucketURL mismatch: %q", loc.bucketURL)
	}
}

func TestParseObjectLocation_S3HTTPSPathStyle(t *testing.T) {
	loc, err := parseObjectLocation("https://s3.example.org/my-bucket/path/to/file.bam", "", ObjectParameters{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if loc.bucket != "my-bucket" {
		t.Fatalf("bucket mismatch: %q", loc.bucket)
	}
	if loc.key != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", loc.key)
	}
}

func TestParseObjectLocation_S3HTTPSVirtualHosted(t *testing.T) {
	loc, err := parseObjectLocation("https://my-bucket.s3.amazonaws.com/path/to/file.bam", "", ObjectParameters{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if loc.bucket != "my-bucket" {
		t.Fatalf("bucket mismatch: %q", loc.bucket)
	}
	if loc.key != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", loc.key)
	}
}

func TestParseObjectLocation_GSScheme(t *testing.T) {
	loc, err := parseObjectLocation("gs://my-gcs-bucket/path/to/file.bam", "", ObjectParameters{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if loc.bucket != "my-gcs-bucket" {
		t.Fatalf("bucket mismatch: %q", loc.bucket)
	}
	if loc.key != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", loc.key)
	}
	if loc.bucketURL != "gs://my-gcs-bucket" {
		t.Fatalf("bucketURL mismatch: %q", loc.bucketURL)
	}
}

func TestParseAzureBlobHTTPS(t *testing.T) {
	loc, err := parseObjectLocation("https://myacct.blob.core.windows.net/mycontainer/path/to/blob.bam", "", ObjectParameters{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if loc.bucket != "mycontainer" {
		t.Fatalf("container mismatch: %q", loc.bucket)
	}
	if loc.key != "path/to/blob.bam" {
		t.Fatalf("key mismatch: %q", loc.key)
	}
	if loc.bucketURL != "azblob://mycontainer?account_name=myacct" {
		t.Fatalf("bucketURL mismatch: %q", loc.bucketURL)
	}
}

func TestParseObjectLocation_S3UsesPassedRegionAndEndpoint(t *testing.T) {
	loc, err := parseObjectLocation("s3://cbds/path/to/file.bin", "", ObjectParameters{
		S3Region:   "us-east-1",
		S3Endpoint: "https://aced-storage.ohsu.edu/",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !strings.Contains(loc.bucketURL, "region=us-east-1") {
		t.Fatalf("expected region query in bucketURL, got %q", loc.bucketURL)
	}
	if !strings.Contains(loc.bucketURL, "endpoint=https%3A%2F%2Faced-storage.ohsu.edu") {
		t.Fatalf("expected endpoint query in bucketURL, got %q", loc.bucketURL)
	}
}

func TestNormalizeSHA256(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	if got := normalizeSHA256(hex); got != hex {
		t.Fatalf("expected %q, got %q", hex, got)
	}

	if got := normalizeSHA256("sha256:" + strings.ToUpper(hex)); got != hex {
		t.Fatalf("expected %q, got %q", hex, got)
	}

	if got := normalizeSHA256("not-a-sha"); got != "" {
		t.Fatalf("expected empty for invalid, got %q", got)
	}
}

func TestExtractSHA256FromMetadata_ByKey(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	md := map[string]string{
		"sha256": hex,
	}
	got := extractSHA256FromMetadata(md)
	if got != hex {
		t.Fatalf("expected %q, got %q", hex, got)
	}
}

func TestExtractSHA256FromMetadata_ByAlternateKey(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	md := map[string]string{
		"checksum-sha256": "sha256:" + hex,
	}
	got := extractSHA256FromMetadata(md)
	if got != hex {
		t.Fatalf("expected %q, got %q", hex, got)
	}
}

func TestExtractSHA256FromMetadata_SearchValues(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	md := map[string]string{
		"something": "sha256:" + hex,
	}
	got := extractSHA256FromMetadata(md)
	if got != hex {
		t.Fatalf("expected %q, got %q", hex, got)
	}
}

// --- test helpers ---

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %v\nerr=%v\nout=%s", name, args, err, string(out))
	}
}

func mustChdir(t *testing.T, dir string) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return old
}
