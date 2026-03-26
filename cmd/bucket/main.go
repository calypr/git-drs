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

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/gitrepo"
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

type putBucketPayload struct {
	Bucket       string `json:"bucket"`
	Region       string `json:"region,omitempty"`
	AccessKey    string `json:"access_key,omitempty"`
	SecretKey    string `json:"secret_key,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	Organization string `json:"organization,omitempty"`
	ProjectID    string `json:"project_id,omitempty"`
}

func normalizeStoragePath(pathValue, bucket string) (string, error) {
	raw := strings.TrimSpace(pathValue)
	if raw == "" {
		return "", nil
	}
	if !strings.HasPrefix(strings.ToLower(raw), "s3://") {
		return "", fmt.Errorf("path must use s3://bucket/prefix format")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid s3 path %q: %w", raw, err)
	}
	if !strings.EqualFold(u.Scheme, "s3") {
		return "", fmt.Errorf("invalid scheme in path %q (expected s3://)", raw)
	}
	host := strings.TrimSpace(u.Host)
	if host == "" {
		return "", fmt.Errorf("invalid s3 path %q: missing bucket", raw)
	}
	if strings.TrimSpace(bucket) != "" && !strings.EqualFold(host, strings.TrimSpace(bucket)) {
		return "", fmt.Errorf("s3 path bucket %q does not match --bucket %q", host, bucket)
	}
	return strings.Trim(strings.TrimSpace(u.Path), "/"), nil
}

var Cmd = &cobra.Command{
	Use:   "bucket",
	Short: "Declare bucket credentials and org/project routing for drs-server",
}

var addCmd = &cobra.Command{
	Use:   "add [remote-name]",
	Short: "Declare bucket credentials and org/project mapping",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("accepts at most one optional remote name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		org := strings.TrimSpace(flagOrg)
		project := strings.TrimSpace(flagProject)
		bucket := strings.TrimSpace(flagBucket)
		prefix, err := normalizeStoragePath(flagPath, bucket)
		if err != nil {
			return err
		}
		if org == "" {
			return fmt.Errorf("--organization is required")
		}
		if project == "" {
			return fmt.Errorf("--project is required")
		}
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
		payload := putBucketPayload{
			Bucket:       bucket,
			Region:       strings.TrimSpace(flagRegion),
			AccessKey:    strings.TrimSpace(flagAccessKey),
			SecretKey:    strings.TrimSpace(flagSecretKey),
			Endpoint:     strings.TrimSpace(flagS3Endpoint),
			Organization: org,
			ProjectID:    project,
		}
		if err := upsertServerBucket(context.Background(), endpoint, token, payload); err != nil {
			return err
		}
		if err := gitrepo.SetBucketMapping(org, project, bucket, prefix); err != nil {
			return fmt.Errorf("failed to persist bucket mapping: %w", err)
		}

		logg.Info("bucket credential and mapping saved", "organization", org, "project", project, "bucket", bucket, "path", prefix)
		return nil
	},
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
				gen3Logger := logs.NewGen3Logger(drslog.GetLogger(), "", remoteName)
				if ensureErr := g3client.EnsureValidCredential(context.Background(), prof, configure, gen3Logger, nil); ensureErr == nil {
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

	resp, err := http.DefaultClient.Do(req)
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

func init() {
	addCmd.Flags().StringVar(&flagOrg, "organization", "", "Organization/program name (required)")
	addCmd.Flags().StringVar(&flagProject, "project", "", "Project ID (required)")
	addCmd.Flags().StringVar(&flagBucket, "bucket", "", "Bucket name (required)")
	addCmd.Flags().StringVar(&flagPath, "path", "", "Optional storage prefix as s3://<bucket>/<prefix>")
	addCmd.Flags().StringVar(&flagRegion, "region", "", "Bucket region (required)")
	addCmd.Flags().StringVar(&flagAccessKey, "access-key", "", "Bucket access key (required)")
	addCmd.Flags().StringVar(&flagSecretKey, "secret-key", "", "Bucket secret key (required)")
	addCmd.Flags().StringVar(&flagS3Endpoint, "s3-endpoint", "", "S3 endpoint for this bucket credential (required)")
	addCmd.Flags().StringVar(&flagDRSURL, "url", "", "DRS server API endpoint (optional if remote configured)")
	addCmd.Flags().StringVar(&flagCred, "cred", "", "Gen3 credential file (optional)")
	addCmd.Flags().StringVar(&flagToken, "token", "", "Bearer token (optional)")
	addCmd.Flags().BoolVar(&flagForce, "force", false, "Overwrite existing local org/project mapping")
	Cmd.AddCommand(addCmd)
}
