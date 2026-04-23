package drs

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/gitrepo"
	syclient "github.com/calypr/syfon/client"
	syrequest "github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/syfonclient"
)

// GetContext returns a pure functional context wrapper bridging git-drs to data-client capability.
func GetContext(profileConfig conf.Credential, remote Gen3Remote, logger *slog.Logger) (*client.GitContext, error) {
	_, err := url.Parse(profileConfig.APIEndpoint)
	if err != nil {
		return nil, err
	}

	projectId := remote.GetProjectId()
	if projectId == "" {
		return nil, fmt.Errorf("no gen3 project specified")
	}

	scope, err := gitrepo.ResolveBucketScope(
		remote.GetOrganization(),
		projectId,
		remote.GetBucketName(),
		remote.GetStoragePrefix(),
	)
	if err != nil {
		return nil, err
	}
	bucketName := scope.Bucket

	rawClient, err := syclient.New(profileConfig.APIEndpoint, syclient.WithBearerToken(profileConfig.AccessToken))
	if err != nil {
		return nil, err
	}
	reqClient, ok := rawClient.(interface{ Requestor() syrequest.Requester })
	if !ok {
		return nil, fmt.Errorf("syfon client does not expose requestor")
	}
	syfonClient, ok := rawClient.(syfonclient.SyfonClient)
	if !ok {
		return nil, fmt.Errorf("syfon client does not implement syfonclient.SyfonClient")
	}

	// Configure the DRS client with git-specific context
	upsert := gitrepo.GetGitConfigBool("drs.upsert", false)
	multiPartThresholdInt := gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)
	var multiPartThreshold int64 = int64(multiPartThresholdInt) * 1024 * 1024
	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	return &client.GitContext{
		Client:             syfonClient,
		Requestor:          reqClient.Requestor(),
		ProjectId:          projectId,
		BucketName:         bucketName,
		Organization:       remote.GetOrganization(),
		StoragePrefix:      scope.Prefix,
		Upsert:             upsert,
		MultiPartThreshold: multiPartThreshold,
		UploadConcurrency:  uploadConcurrency,
		Logger:             logger,
		Credential:         &profileConfig,
	}, nil
}
