package addurl

import (
	"fmt"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/gitrepo"
)

func resolveTargetScope(remoteConfig config.DRSRemote) (organization string, project string, scope gitrepo.ResolvedBucketScope, err error) {
	organization = remoteConfig.GetOrganization()
	project = remoteConfig.GetProjectId()
	if project == "" {
		return "", "", gitrepo.ResolvedBucketScope{}, fmt.Errorf("target project is required (set remote project)")
	}

	scope, err = gitrepo.ResolveBucketScope(
		organization,
		project,
		remoteConfig.GetBucketName(),
		remoteConfig.GetStoragePrefix(),
	)
	if err != nil {
		return "", "", gitrepo.ResolvedBucketScope{}, err
	}
	return organization, project, scope, nil
}
