package add

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	bucketapi "github.com/calypr/syfon/apigen/client/bucketapi"
	syfoncommon "github.com/calypr/syfon/common"
	"github.com/spf13/cobra"
)

var LocalCmd = &cobra.Command{
	Use:   "local <remote-name> <url> <organization/project>",
	Short: "Add a local DRS server",
	Long:  "Add a local DRS server by specifying its base URL and scope. Optional --username/--password configures basic auth for helper flows.",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		remoteName := args[0]
		url := args[1]
		scopeArg := args[2]

		if err := initialize.EnsureInitialized(drslog.GetLogger()); err != nil {
			return fmt.Errorf("failed to initialize repository: %w", err)
		}
		if url == "" {
			return fmt.Errorf("URL cannot be empty")
		}
		organization, project, err := parseScopeArg(scopeArg)
		if err != nil {
			return err
		}
		scope, err := gitrepo.ResolveBucketScope(organization, project, "", "")
		if err != nil {
			scope, err = resolveBucketScopeFromLocalServer(context.Background(), url, strings.TrimSpace(localUsername), strings.TrimSpace(localPassword), organization, project)
			if err != nil {
				return fmt.Errorf("failed resolving bucket mapping for organization=%q project=%q: %w", organization, project, err)
			}
		}
		resolvedBucket := strings.TrimSpace(scope.Bucket)
		resolvedStoragePrefix := strings.TrimSpace(scope.Prefix)
		if resolvedBucket == "" {
			return fmt.Errorf("no bucket mapping found for organization=%q project=%q", organization, project)
		}

		remoteSelect := config.RemoteSelect{
			Local: &config.LocalRemote{
				BaseURL:       url,
				ProjectID:     project,
				Bucket:        resolvedBucket,
				Organization:  organization,
				StoragePrefix: resolvedStoragePrefix,
			},
		}

		newConfig, err := config.UpdateRemote(config.Remote(remoteName), remoteSelect)
		if err != nil {
			return err
		}
		if err := gitrepo.SetRemoteLFSURL(remoteName, url); err != nil {
			return fmt.Errorf("failed to configure lfs url for remote %q: %w", remoteName, err)
		}
		if err := gitrepo.ConfigureCredentialHelperForRepo(); err != nil {
			return fmt.Errorf("failed to configure git credential helper: %w", err)
		}
		if strings.TrimSpace(localUsername) != "" || strings.TrimSpace(localPassword) != "" {
			if strings.TrimSpace(localUsername) == "" || strings.TrimSpace(localPassword) == "" {
				return fmt.Errorf("both --username and --password are required when configuring local basic auth")
			}
			if err := gitrepo.SetRemoteBasicAuth(remoteName, strings.TrimSpace(localUsername), strings.TrimSpace(localPassword)); err != nil {
				return fmt.Errorf("failed to configure local basic auth for remote %q: %w", remoteName, err)
			}
		}

		fmt.Printf("Added remote '%s'. Config: %v\n", remoteName, newConfig.GetRemote(config.Remote(remoteName)))
		return nil
	},
}

func resolveBucketScopeFromLocalServer(ctx context.Context, endpoint, username, password, organization, project string) (gitrepo.ResolvedBucketScope, error) {
	if strings.TrimSpace(endpoint) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing API endpoint for server bucket lookup")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/data/buckets", nil)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("build bucket list request: %w", err)
	}
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("request bucket list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("bucket list failed with status %d", resp.StatusCode)
	}

	var payload bucketapi.BucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("decode bucket list response: %w", err)
	}

	projectResource, err := syfoncommon.ResourcePath(organization, project)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, err
	}
	orgResource, err := syfoncommon.ResourcePath(organization, "")
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, err
	}

	if bucket, ok := findBucketByResource(payload, projectResource); ok {
		return gitrepo.ResolvedBucketScope{Bucket: bucket}, nil
	}
	if bucket, ok := findBucketByResource(payload, orgResource); ok {
		return gitrepo.ResolvedBucketScope{Bucket: bucket}, nil
	}

	return gitrepo.ResolvedBucketScope{}, fmt.Errorf("no visible server bucket matched organization=%q project=%q", organization, project)
}
