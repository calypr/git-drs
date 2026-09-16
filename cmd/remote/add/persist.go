package add

import (
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/gitrepo"
)

func persistGen3Remote(remoteName, organization, project, endpoint string, scope gitrepo.ResolvedBucketScope, saveCredential func() error) error {
	remote := config.Remote(remoteName)
	remoteGen3 := config.RemoteSelect{
		Gen3: &config.Gen3Remote{
			Endpoint:      endpoint,
			ProjectID:     project,
			Organization:  organization,
			Bucket:        strings.TrimSpace(scope.Bucket),
			StoragePrefix: strings.TrimSpace(scope.Prefix),
		},
	}

	if _, err := config.UpdateRemote(remote, remoteGen3); err != nil {
		return fmt.Errorf("failed to update remote config: %w", err)
	}
	if err := saveCredential(); err != nil {
		return fmt.Errorf("failed to configure/update Gen3 profile: %w", err)
	}
	if err := configureRepoRemote(remoteName, endpoint); err != nil {
		return err
	}
	return nil
}

func persistLocalRemote(remoteName, url, organization, project string, scope gitrepo.ResolvedBucketScope) (*config.Config, error) {
	remoteSelect := config.RemoteSelect{
		Local: &config.LocalRemote{
			BaseURL:       url,
			ProjectID:     project,
			Bucket:        strings.TrimSpace(scope.Bucket),
			Organization:  organization,
			StoragePrefix: strings.TrimSpace(scope.Prefix),
		},
	}

	newConfig, err := config.UpdateRemote(config.Remote(remoteName), remoteSelect)
	if err != nil {
		return nil, err
	}
	if err := configureRepoRemote(remoteName, url); err != nil {
		return nil, err
	}
	return newConfig, nil
}

func configureRepoRemote(remoteName, endpoint string) error {
	if err := gitrepo.SetRemoteLFSURL(remoteName, endpoint); err != nil {
		return fmt.Errorf("failed to configure lfs url for remote %q: %w", remoteName, err)
	}
	if err := gitrepo.ConfigureCredentialHelperForRepo(); err != nil {
		return fmt.Errorf("failed to configure git credential helper: %w", err)
	}
	return nil
}

func configureLocalBasicAuth(remoteName, username, password string) error {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" && password == "" {
		return nil
	}
	if username == "" || password == "" {
		return fmt.Errorf("both --username and --password are required when configuring local basic auth")
	}
	if err := gitrepo.SetRemoteBasicAuth(remoteName, username, password); err != nil {
		return fmt.Errorf("failed to configure local basic auth for remote %q: %w", remoteName, err)
	}
	return nil
}
