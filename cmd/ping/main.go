package ping

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/remoteruntime"
	bucketapi "github.com/calypr/syfon/apigen/client/bucketapi"
	syservices "github.com/calypr/syfon/client/services"
	syfoncommon "github.com/calypr/syfon/common"
	"github.com/spf13/cobra"
)

type statusInfo struct {
	Remote        config.Remote
	IsDefault     bool
	RemoteType    string
	Endpoint      string
	Organization  string
	Project       string
	Bucket        string
	StoragePrefix string
	AuthMode      string
}

type healthInfo struct {
	ServiceInfo string
}

var pingHealth = func(ctx context.Context, gc *remoteruntime.GitContext) (healthInfo, error) {
	if gc != nil && gc.RemoteType == config.TerraServerType {
		serviceInfo, err := pingTerraServiceInfo(ctx, gc.Endpoint)
		return healthInfo{ServiceInfo: serviceInfo}, err
	}
	return healthInfo{}, gc.Client.Health().Ping(ctx)
}

var pingScopeAccess = func(ctx context.Context, gc *remoteruntime.GitContext) (scopeAccessInfo, error) {
	return checkScopeAccess(ctx, gc)
}

type scopeAccessInfo struct {
	Checked         bool
	VisibleBucket   string
	ProjectReadable bool
}

var Cmd = &cobra.Command{
	Use:   "ping [remote-name]",
	Short: "Show effective remote setup and verify the remote responds",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs ping --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()
		status, gc, err := resolveStatus(args, logger)
		if err != nil {
			return err
		}
		printStatus(status)

		health, err := pingHealth(cmd.Context(), gc)
		if err != nil {
			return fmt.Errorf("remote health check failed for %q (%s): %w", status.Remote, status.Endpoint, err)
		}
		fmt.Println("health: ok")
		if strings.TrimSpace(health.ServiceInfo) != "" {
			fmt.Printf("service-info: %s\n", health.ServiceInfo)
		}

		scopeInfo, err := pingScopeAccess(cmd.Context(), gc)
		if err != nil {
			return fmt.Errorf("configured scope access check failed for remote %q (organization=%s project=%s bucket=%s): %w",
				status.Remote,
				blankIfEmpty(status.Organization),
				blankIfEmpty(status.Project),
				blankIfEmpty(status.Bucket),
				err,
			)
		}
		if scopeInfo.Checked {
			fmt.Println("scope_access: ok")
			if strings.TrimSpace(scopeInfo.VisibleBucket) != "" {
				fmt.Printf("visible_bucket: %s\n", scopeInfo.VisibleBucket)
			}
			if scopeInfo.ProjectReadable {
				fmt.Println("project_access: readable")
			}
		}
		return nil
	},
}

func resolveStatus(args []string, logger *slog.Logger) (statusInfo, *remoteruntime.GitContext, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return statusInfo{}, nil, err
	}

	var remoteArg string
	if len(args) == 1 {
		remoteArg = args[0]
	}
	remoteName, err := cfg.GetRemoteOrDefault(remoteArg)
	if err != nil {
		return statusInfo{}, nil, err
	}

	remoteCfg := cfg.GetRemote(remoteName)
	if remoteCfg == nil {
		return statusInfo{}, nil, fmt.Errorf("no remote configuration found for %q", remoteName)
	}

	gc, err := remoteruntime.New(cfg, remoteName, logger)
	if err != nil {
		return statusInfo{}, nil, err
	}

	status := statusInfo{
		Remote:        remoteName,
		IsDefault:     remoteName == cfg.DefaultRemote,
		Endpoint:      remoteCfg.GetEndpoint(),
		Organization:  remoteCfg.GetOrganization(),
		Project:       remoteCfg.GetProjectId(),
		Bucket:        gc.BucketName,
		StoragePrefix: gc.StoragePrefix,
		AuthMode:      authMode(gc),
	}
	switch remoteCfg.(type) {
	case *config.Gen3Remote:
		status.RemoteType = string(config.Gen3ServerType)
	case *config.LocalRemote:
		status.RemoteType = string(config.LocalServerType)
	case *config.TerraRemote:
		status.RemoteType = string(config.TerraServerType)
	default:
		status.RemoteType = "unknown"
	}

	return status, gc, nil
}

func printStatus(status statusInfo) {
	def := ""
	if status.IsDefault {
		def = " (default)"
	}
	fmt.Printf("remote: %s%s\n", status.Remote, def)
	fmt.Printf("type: %s\n", status.RemoteType)
	fmt.Printf("endpoint: %s\n", status.Endpoint)
	fmt.Printf("organization: %s\n", blankIfEmpty(status.Organization))
	fmt.Printf("project: %s\n", blankIfEmpty(status.Project))
	fmt.Printf("bucket: %s\n", blankIfEmpty(status.Bucket))
	fmt.Printf("storage_prefix: %s\n", blankIfEmpty(status.StoragePrefix))
	fmt.Printf("auth: %s\n", status.AuthMode)
}

func authMode(gc *remoteruntime.GitContext) string {
	if gc == nil || gc.Credential == nil {
		return "none"
	}
	if strings.TrimSpace(gc.Credential.AccessToken) != "" {
		return "bearer"
	}
	if strings.TrimSpace(gc.Credential.KeyID) != "" || strings.TrimSpace(gc.Credential.APIKey) != "" {
		return "basic"
	}
	return "none"
}

func blankIfEmpty(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "-"
	}
	return v
}

func checkScopeAccess(ctx context.Context, gc *remoteruntime.GitContext) (scopeAccessInfo, error) {
	info := scopeAccessInfo{}
	if gc == nil {
		return scopeAccessInfo{}, fmt.Errorf("DRS client unavailable")
	}
	organization := strings.TrimSpace(gc.Organization)
	project := strings.TrimSpace(gc.ProjectId)
	bucket := strings.TrimSpace(gc.BucketName)

	if organization == "" && project == "" && bucket == "" {
		return info, nil
	}
	if gc.Client == nil {
		return scopeAccessInfo{}, fmt.Errorf("DRS client unavailable")
	}
	info.Checked = true

	if organization != "" && project != "" {
		visibleBucket, err := visibleBucketForScope(ctx, gc, organization, project)
		if err != nil {
			return scopeAccessInfo{}, err
		}
		info.VisibleBucket = visibleBucket
		if bucket != "" && !strings.EqualFold(strings.TrimSpace(visibleBucket), bucket) {
			return scopeAccessInfo{}, fmt.Errorf("server exposes bucket %q for configured scope, but repo is configured for bucket %q", visibleBucket, bucket)
		}
	}

	if project != "" {
		if _, err := gc.Client.Index().List(ctx, syservices.ListRecordsOptions{
			Organization: organization,
			ProjectID:    project,
			Limit:        1,
			Page:         1,
		}); err != nil {
			return scopeAccessInfo{}, fmt.Errorf("project listing failed: %w", err)
		}
		info.ProjectReadable = true
	}

	return info, nil
}

func pingTerraServiceInfo(ctx context.Context, endpoint string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	serviceInfoURL, err := terraServiceInfoURL(endpoint)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceInfoURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	serviceInfo, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("terra DRS service-info returned %s", resp.Status)
	}
	if readErr != nil {
		return "", readErr
	}
	return strings.TrimSpace(string(serviceInfo)), nil
}

func terraServiceInfoURL(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("terra endpoint is empty")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("terra endpoint must be an absolute URL: %q", endpoint)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ga4gh/drs/v1/service-info"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func visibleBucketForScope(ctx context.Context, gc *remoteruntime.GitContext, organization, project string) (string, error) {
	payload, err := gc.Client.Buckets().List(ctx)
	if err != nil {
		return "", fmt.Errorf("bucket visibility lookup failed: %w", err)
	}

	resource, err := syfoncommon.ResourcePath(organization, project)
	if err != nil {
		return "", fmt.Errorf("build scope resource path: %w", err)
	}

	matches := findBucketsByResource(payload, resource)
	if len(matches) == 0 {
		return "", fmt.Errorf("no visible server bucket matched configured scope %s", resource)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple visible server buckets matched configured scope %s: %s", resource, strings.Join(matches, ", "))
	}
	return matches[0], nil
}

func findBucketsByResource(payload bucketapi.BucketsResponse, resource string) []string {
	resource = syfoncommon.NormalizeAccessResource(resource)
	if resource == "" {
		return nil
	}

	matches := make([]string, 0)
	for bucket, meta := range payload.S3BUCKETS {
		if meta.Programs == nil {
			continue
		}
		for _, candidate := range *meta.Programs {
			if syfoncommon.NormalizeAccessResource(candidate) == resource {
				matches = append(matches, bucket)
				break
			}
		}
	}
	return matches
}
