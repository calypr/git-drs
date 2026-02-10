package cloud

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseS3URL_S3Scheme(t *testing.T) {
	b, k, err := parseS3URL("s3://my-bucket/path/to/file.bam")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if b != "my-bucket" {
		t.Fatalf("bucket mismatch: %q", b)
	}
	if k != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", k)
	}
}

func TestParseS3URL_HTTPSPathStyle(t *testing.T) {
	b, k, err := parseS3URL("https://s3.example.org/my-bucket/path/to/file.bam")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if b != "my-bucket" {
		t.Fatalf("bucket mismatch: %q", b)
	}
	if k != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", k)
	}
}

func TestParseS3URL_HTTPSVirtualHosted(t *testing.T) {
	b, k, err := parseS3URL("https://my-bucket.s3.amazonaws.com/path/to/file.bam")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if b != "my-bucket" {
		t.Fatalf("bucket mismatch: %q", b)
	}
	if k != "path/to/file.bam" {
		t.Fatalf("key mismatch: %q", k)
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
