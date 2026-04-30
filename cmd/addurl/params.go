package addurl

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
	sycloud "github.com/calypr/syfon/client/cloud"
	"github.com/spf13/cobra"
)

// addURLInput holds the parsed CLI state for the add-url command.
type addURLInput struct {
	sourceArg string
	objectURL string
	path      string
	sha256    string
	scheme    string
}

// parseAddURLInput parses CLI args and flags into an addURLInput.
func parseAddURLInput(cmd *cobra.Command, args []string) (addURLInput, error) {
	sourceArg := strings.TrimSpace(args[0])

	pathArg, err := resolvePathArg(sourceArg, args)
	if err != nil {
		return addURLInput{}, err
	}

	sha256Param, err := cmd.Flags().GetString("sha256")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag sha256: %w", err)
	}
	scheme, err := cmd.Flags().GetString("scheme")
	if err != nil {
		return addURLInput{}, fmt.Errorf("read flag scheme: %w", err)
	}

	return addURLInput{
		sourceArg: sourceArg,
		path:      pathArg,
		sha256:    sha256Param,
		scheme:    strings.ToLower(strings.TrimSpace(scheme)),
	}, nil
}

// resolvePathArg returns the explicit destination path argument when provided,
// otherwise derives the worktree path from the given cloud URL or object key.
func resolvePathArg(sourceArg string, args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	if looksLikeCloudURL(sourceArg) {
		u, err := url.Parse(sourceArg)
		if err != nil {
			return "", err
		}
		return strings.TrimPrefix(u.Path, "/"), nil
	}
	return strings.Trim(strings.TrimSpace(sourceArg), "/"), nil
}

func buildObjectParameters(objectURL, pathArg, sha256 string) sycloud.ObjectParameters {
	return sycloud.ObjectParameters{
		ObjectURL:       objectURL,
		S3Region:        firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), os.Getenv("TEST_BUCKET_REGION")),
		S3Endpoint:      firstNonEmpty(os.Getenv("AWS_ENDPOINT_URL_S3"), os.Getenv("AWS_ENDPOINT_URL"), os.Getenv("TEST_BUCKET_ENDPOINT")),
		S3AccessKey:     firstNonEmpty(os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("TEST_BUCKET_ACCESS_KEY")),
		S3SecretKey:     firstNonEmpty(os.Getenv("AWS_SECRET_ACCESS_KEY"), os.Getenv("TEST_BUCKET_SECRET_KEY")),
		SHA256:          sha256,
		DestinationPath: pathArg,
	}
}

func looksLikeCloudURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if strings.TrimSpace(u.Scheme) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "s3", "gs", "gcs", "azblob", "http", "https":
		return strings.TrimSpace(u.Host) != ""
	default:
		return false
	}
}

func resolveObjectURL(input addURLInput, scope gitrepo.ResolvedBucketScope) (string, error) {
	if looksLikeCloudURL(input.sourceArg) {
		return input.sourceArg, nil
	}
	if input.scheme == "" {
		return "", fmt.Errorf("object key mode requires --scheme because local bucket mappings store bucket/prefix but not provider scheme")
	}
	key := joinObjectKey(scope.Prefix, input.sourceArg)
	switch input.scheme {
	case "s3":
		return fmt.Sprintf("s3://%s/%s", scope.Bucket, key), nil
	case "gs", "gcs":
		return fmt.Sprintf("gs://%s/%s", scope.Bucket, key), nil
	case "azblob", "az":
		return "", fmt.Errorf("object key mode for Azure requires a full azblob:// URL because the local mapping does not store account_name")
	default:
		return "", fmt.Errorf("unsupported --scheme %q (expected s3 or gs, or pass a full object URL)", input.scheme)
	}
}

func joinObjectKey(prefix, key string) string {
	parts := make([]string, 0, 2)
	if p := strings.Trim(strings.TrimSpace(prefix), "/"); p != "" {
		parts = append(parts, p)
	}
	if k := strings.Trim(strings.TrimSpace(key), "/"); k != "" {
		parts = append(parts, k)
	}
	return path.Join(parts...)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}
