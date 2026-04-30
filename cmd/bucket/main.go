package bucket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/calypr/data-client/credentials"
	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	conf "github.com/calypr/syfon/client/config"
	"github.com/spf13/cobra"
)

var (
	flagOrg        string
	flagProject    string
	flagBucket     string
	flagPath       string
	flagRegion     string
	flagAccessKey  string
	flagSecretKey  string
	flagS3Endpoint string
	flagDRSURL     string
	flagCred       string
	flagToken      string
	flagForce      bool
)

const defaultBucketAPITimeout = 30 * time.Second

type putBucketPayload struct {
	Bucket    string `json:"bucket"`
	Region    string `json:"region,omitempty"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
}

type addBucketScopePayload struct {
	Organization string `json:"organization"`
	ProjectID    string `json:"project_id"`
	Path         string `json:"path,omitempty"`
}

func normalizeStoragePath(pathValue, bucket string) (string, error) {
	raw := strings.TrimSpace(pathValue)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid storage path %q: %w", raw, err)
	}
	switch strings.ToLower(strings.TrimSpace(u.Scheme)) {
	case "s3", "gs", "gcs", "az", "azblob":
	default:
		return "", fmt.Errorf("path must use s3://, gs://, or azblob:// storage URL format")
	}
	host := strings.TrimSpace(u.Host)
	if host == "" {
		return "", fmt.Errorf("invalid storage path %q: missing bucket", raw)
	}
	if strings.TrimSpace(bucket) != "" && !strings.EqualFold(host, strings.TrimSpace(bucket)) {
		return "", fmt.Errorf("storage path bucket %q does not match expected bucket %q", host, bucket)
	}
	return strings.Trim(strings.TrimSpace(u.Path), "/"), nil
}

func bucketFromStoragePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("--path is required and must include the bucket URL")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid --path %q: %w", raw, err)
	}
	if strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return "", fmt.Errorf("--path must be a storage URL like s3://bucket/prefix")
	}
	return strings.TrimSpace(u.Host), nil
}

var Cmd = &cobra.Command{
	Use:   "bucket",
	Short: "Declare bucket credentials and org/project routing for drs-server",
}

var addCmd = &cobra.Command{
	Use:   "add [remote-name]",
	Short: "Declare bucket credentials on the server",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("accepts at most one optional remote name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		bucket := strings.TrimSpace(flagBucket)
		if bucket == "" {
			return fmt.Errorf("--bucket is required")
		}
		if strings.TrimSpace(flagRegion) == "" {
			return fmt.Errorf("--region is required")
		}
		if strings.TrimSpace(flagAccessKey) == "" {
			return fmt.Errorf("--access-key is required")
		}
		if strings.TrimSpace(flagSecretKey) == "" {
			return fmt.Errorf("--secret-key is required")
		}
		if strings.TrimSpace(flagS3Endpoint) == "" {
			return fmt.Errorf("--s3-endpoint is required")
		}

		remoteName := "origin"
		if len(args) == 1 {
			remoteName = strings.TrimSpace(args[0])
		}
		endpoint, token, err := resolveEndpointAndToken(remoteName)
		if err != nil {
			return err
		}
		payload := putBucketPayload{
			Bucket:    bucket,
			Region:    strings.TrimSpace(flagRegion),
			AccessKey: strings.TrimSpace(flagAccessKey),
			SecretKey: strings.TrimSpace(flagSecretKey),
			Endpoint:  strings.TrimSpace(flagS3Endpoint),
		}
		if err := upsertServerBucket(context.Background(), endpoint, token, payload); err != nil {
			return err
		}

		logg.Info("bucket credential saved", "bucket", bucket)
		return nil
	},
}

var addOrganizationCmd = &cobra.Command{
	Use:   "add-organization [remote-name]",
	Short: "Declare organization/program bucket mapping",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("accepts at most one optional remote name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return addScope(args, false)
	},
}

var addProjectCmd = &cobra.Command{
	Use:   "add-project [remote-name]",
	Short: "Declare project bucket mapping",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("accepts at most one optional remote name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return addScope(args, true)
	},
}

func addScope(args []string, requireProject bool) error {
	logg := drslog.GetLogger()
	org := strings.TrimSpace(flagOrg)
	project := strings.TrimSpace(flagProject)
	scopePath := strings.TrimSpace(flagPath)
	bucket, err := bucketFromStoragePath(scopePath)
	if err != nil {
		return err
	}
	prefix, err := normalizeStoragePath(scopePath, bucket)
	if err != nil {
		return err
	}
	if org == "" {
		return fmt.Errorf("--organization is required")
	}
	if requireProject && project == "" {
		return fmt.Errorf("--project is required")
	}
	if !requireProject && project != "" {
		return fmt.Errorf("--project is not valid for add-organization; use add-project")
	}
	if existing, ok, err := gitrepo.GetBucketMapping(org, project); err != nil {
		return fmt.Errorf("failed checking existing mapping: %w", err)
	} else if ok && !flagForce {
		return fmt.Errorf("mapping already exists for organization=%q project=%q (bucket=%q prefix=%q); use --force to overwrite", org, project, existing.Bucket, existing.Prefix)
	}

	remoteName := "origin"
	if len(args) == 1 {
		remoteName = strings.TrimSpace(args[0])
	}
	endpoint, token, err := resolveEndpointAndToken(remoteName)
	if err != nil {
		return err
	}
	if err := addServerBucketScope(context.Background(), endpoint, token, bucket, addBucketScopePayload{
		Organization: org,
		ProjectID:    project,
		Path:         scopePath,
	}); err != nil {
		return err
	}
	if err := gitrepo.SetBucketMapping(org, project, bucket, prefix); err != nil {
		return fmt.Errorf("failed to persist bucket mapping: %w", err)
	}
	logg.Info("bucket mapping saved", "organization", org, "project", project, "bucket", bucket, "path", prefix)
	return nil
}

func resolveEndpointAndToken(remoteName string) (string, string, error) {
	configure := conf.NewConfigure(drslog.GetLogger())

	// Resolve token
	token := strings.TrimSpace(flagToken)
	if token == "" && strings.TrimSpace(flagCred) != "" {
		cred, err := configure.Import(flagCred, "")
		if err != nil {
			return "", "", fmt.Errorf("failed to read credential file: %w", err)
		}
		token = strings.TrimSpace(cred.AccessToken)
		if token == "" {
			token = strings.TrimSpace(cred.APIKey)
		}
	}
	if token == "" {
		if repoToken, err := gitrepo.GetRemoteToken(remoteName); err == nil {
			token = strings.TrimSpace(repoToken)
		}
	}
	if token == "" {
		if prof, err := configure.Load(remoteName); err == nil {
			token = strings.TrimSpace(prof.AccessToken)
			if token == "" {
				if ensureErr := credentials.EnsureValidCredential(context.Background(), prof, drslog.GetLogger()); ensureErr == nil {
					_ = configure.Save(prof)
					token = strings.TrimSpace(prof.AccessToken)
				}
			}
		}
	}
	if token == "" {
		return "", "", fmt.Errorf("unable to resolve token: pass --token or --cred, or configure remote auth first")
	}

	endpoint := strings.TrimSpace(flagDRSURL)
	if endpoint == "" {
		var err error
		endpoint, err = gitrepo.GetGitConfigString(fmt.Sprintf("drs.remote.%s.endpoint", remoteName))
		if err != nil {
			return "", "", fmt.Errorf("failed to resolve endpoint from git config: %w", err)
		}
		endpoint = strings.TrimSpace(endpoint)
	}
	if endpoint == "" {
		parsed, err := common.ParseAPIEndpointFromToken(token)
		if err != nil {
			return "", "", fmt.Errorf("unable to resolve API endpoint from token: %w", err)
		}
		endpoint = strings.TrimSpace(parsed)
	}
	if endpoint == "" {
		return "", "", fmt.Errorf("unable to resolve API endpoint")
	}
	return strings.TrimRight(endpoint, "/"), token, nil
}

func upsertServerBucket(ctx context.Context, endpoint, token string, payload putBucketPayload) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultBucketAPITimeout)
		defer cancel()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode bucket request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint+"/data/buckets", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: defaultBucketAPITimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("bucket credential upsert request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyText, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if msg := strings.TrimSpace(string(bodyText)); msg != "" {
			return fmt.Errorf("bucket credential upsert failed with status %d: %s", resp.StatusCode, msg)
		}
		return fmt.Errorf("bucket credential upsert failed with status %d", resp.StatusCode)
	}
	return nil
}

func addServerBucketScope(ctx context.Context, endpoint, token, bucket string, payload addBucketScopePayload) error {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultBucketAPITimeout)
		defer cancel()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode bucket scope request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/data/buckets/"+url.PathEscape(bucket)+"/scopes", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: defaultBucketAPITimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("bucket scope request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		bodyText, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if msg := strings.TrimSpace(string(bodyText)); msg != "" {
			return fmt.Errorf("bucket scope failed with status %d: %s", resp.StatusCode, msg)
		}
		return fmt.Errorf("bucket scope failed with status %d", resp.StatusCode)
	}
	return nil
}

func init() {
	addCmd.Flags().StringVar(&flagBucket, "bucket", "", "Bucket name (required)")
	addCmd.Flags().StringVar(&flagRegion, "region", "", "Bucket region (required)")
	addCmd.Flags().StringVar(&flagAccessKey, "access-key", "", "Bucket access key (required)")
	addCmd.Flags().StringVar(&flagSecretKey, "secret-key", "", "Bucket secret key (required)")
	addCmd.Flags().StringVar(&flagS3Endpoint, "s3-endpoint", "", "S3 endpoint for this bucket credential (required)")
	addCmd.Flags().StringVar(&flagDRSURL, "url", "", "DRS server API endpoint (optional if remote configured)")
	addCmd.Flags().StringVar(&flagCred, "cred", "", "Gen3 credential file (optional)")
	addCmd.Flags().StringVar(&flagToken, "token", "", "Bearer token (optional)")

	for _, scopeCmd := range []*cobra.Command{addOrganizationCmd, addProjectCmd} {
		scopeCmd.Flags().StringVar(&flagOrg, "organization", "", "Organization/program name (required)")
		scopeCmd.Flags().StringVar(&flagProject, "project", "", "Project ID (required for add-project)")
		scopeCmd.Flags().StringVar(&flagPath, "path", "", "Storage root as <scheme>://<bucket>/<prefix>")
		scopeCmd.Flags().StringVar(&flagDRSURL, "url", "", "DRS server API endpoint (optional if remote configured)")
		scopeCmd.Flags().StringVar(&flagCred, "cred", "", "Gen3 credential file (optional)")
		scopeCmd.Flags().StringVar(&flagToken, "token", "", "Bearer token (optional)")
		scopeCmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite existing local org/project mapping")
	}

	Cmd.AddCommand(addCmd)
	Cmd.AddCommand(addOrganizationCmd)
	Cmd.AddCommand(addProjectCmd)
}
