package add

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/calypr/data-client/credentials"
	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
	bucketapi "github.com/calypr/syfon/apigen/client/bucketapi"
	syfoncommon "github.com/calypr/syfon/common"
	conf "github.com/calypr/syfon/client/config"
	"github.com/spf13/cobra"
)

var Gen3Cmd = &cobra.Command{
	Use: "gen3 [remote-name]",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs remote add gen3 --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logg := drslog.GetLogger()

		remoteName := config.ORIGIN
		if len(args) > 0 {
			remoteName = args[0]
		}

		err := gen3Init(remoteName, credFile, fenceToken, project, organization, logg)
		if err != nil {
			return fmt.Errorf("error configuring gen3 server: %v", err)
		}
		return nil
	},
}

func gen3Init(remoteName, credFile, fenceToken, project, organization string, logg *slog.Logger) error {
	if remoteName == "" {
		return fmt.Errorf("remote name is required")
	}
	if project == "" {
		return fmt.Errorf("project is required for Gen3 remote")
	}

	organization, project = common.ParseOrgProject(strings.TrimSpace(organization), strings.TrimSpace(project))
	if organization == "" {
		return fmt.Errorf("organization is required (or use a project id in <org>-<project> form so it can be inferred)")
	}

	var accessToken, apiKey, keyID, apiEndpoint string
	configure := conf.NewConfigure(logg)
	switch {
	case fenceToken != "":
		accessToken = fenceToken
		var err error
		apiEndpoint, err = common.ParseAPIEndpointFromToken(accessToken)
		if err != nil {
			return fmt.Errorf("failed to parse API endpoint from provided access token: %w", err)
		}

	case credFile != "":
		cred, err := configure.Import(credFile, "")
		if err != nil {
			return fmt.Errorf("failed to read credentials file %s: %w", credFile, err)
		}
		accessToken = cred.AccessToken
		apiKey = cred.APIKey
		keyID = cred.KeyID

		apiEndpoint, err = common.ParseAPIEndpointFromToken(cred.APIKey)
		if err != nil {
			return fmt.Errorf("failed to parse API endpoint from API key in credentials file: %w", err)
		}

	default:
		existing, err := configure.Load(remoteName)
		if err != nil {
			return fmt.Errorf("failed to load %s config: %w", remoteName, err)
		} else {
			accessToken = existing.AccessToken
			apiKey = existing.APIKey
			keyID = existing.KeyID
			apiEndpoint = existing.APIEndpoint
		}
	}

	if apiEndpoint == "" {
		return fmt.Errorf("could not determine Gen3 API endpoint")
	}

	cred := &conf.Credential{
		Profile:            remoteName,
		APIEndpoint:        apiEndpoint,
		APIKey:             apiKey,
		KeyID:              keyID,
		AccessToken:        accessToken, // may be stale
		UseShepherd:        "false",
		MinShepherdVersion: "",
	}

	if err := credentials.EnsureValidCredential(context.Background(), cred, logg); err != nil {
		return fmt.Errorf("failed to verify/refresh Gen3 credential: %w", err)
	}

	scope, err := gitrepo.ResolveBucketScope(organization, project, "", "")
	if err != nil {
		scope, err = resolveBucketScopeFromServer(context.Background(), apiEndpoint, strings.TrimSpace(cred.AccessToken), organization, project)
		if err != nil {
			return fmt.Errorf("failed resolving bucket mapping for organization=%q project=%q: %w", organization, project, err)
		}
	}
	resolvedBucket := strings.TrimSpace(scope.Bucket)
	resolvedStoragePrefix := strings.TrimSpace(scope.Prefix)
	if resolvedBucket == "" {
		return fmt.Errorf("no bucket mapping found for organization=%q project=%q", organization, project)
	}

	remoteGen3 := config.RemoteSelect{
		Gen3: &config.Gen3Remote{
			Endpoint:      apiEndpoint,
			ProjectID:     project,
			Organization:  organization,
			Bucket:        resolvedBucket,
			StoragePrefix: resolvedStoragePrefix,
		},
	}

	remote := config.Remote(remoteName)
	if _, err := config.UpdateRemote(remote, remoteGen3); err != nil {
		return fmt.Errorf("failed to update remote config: %w", err)
	}
	logg.Debug(fmt.Sprintf("Remote added/updated: %s → %s (project: %s, bucket: %s, storage_prefix: %s)", remoteName, apiEndpoint, project, resolvedBucket, resolvedStoragePrefix))

	if err := configure.Save(cred); err != nil {
		return fmt.Errorf("failed to configure/update Gen3 profile: %w", err)
	}
	// Configure stock git credential plumbing for lfs + persist the refreshed token locally.
	if err := gitrepo.ConfigureCredentialHelperForRepo(); err != nil {
		return fmt.Errorf("failed to configure git credential helper: %w", err)
	}
	if err := gitrepo.SetRemoteLFSURL(remoteName, apiEndpoint); err != nil {
		return fmt.Errorf("failed to set lfs url for remote %s: %w", remoteName, err)
	}
	if strings.TrimSpace(cred.AccessToken) != "" {
		if err := gitrepo.SetRemoteToken(remoteName, strings.TrimSpace(cred.AccessToken)); err != nil {
			return fmt.Errorf("failed to persist repo token for remote %s: %w", remoteName, err)
		}
	}

	logg.Debug(fmt.Sprintf("Gen3 profile '%s' configured and token refreshed successfully", remoteName))
	return nil
}

func resolveBucketScopeFromServer(ctx context.Context, endpoint, token, organization, project string) (gitrepo.ResolvedBucketScope, error) {
	if strings.TrimSpace(endpoint) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing API endpoint for server bucket lookup")
	}
	if strings.TrimSpace(token) == "" {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("missing access token for server bucket lookup")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/data/buckets", nil)
	if err != nil {
		return gitrepo.ResolvedBucketScope{}, fmt.Errorf("build bucket list request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))

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

func findBucketByResource(payload bucketapi.BucketsResponse, resource string) (string, bool) {
	resource = syfoncommon.NormalizeAccessResource(resource)
	if resource == "" {
		return "", false
	}
	var match string
	for bucket, meta := range payload.S3BUCKETS {
		if meta.Programs == nil {
			continue
		}
		for _, candidate := range *meta.Programs {
			if syfoncommon.NormalizeAccessResource(candidate) != resource {
				continue
			}
			if match != "" && match != bucket {
				return "", false
			}
			match = bucket
			break
		}
	}
	if match == "" {
		return "", false
	}
	return match, true
}
