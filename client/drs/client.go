package drs

import (
	"fmt"
	"log/slog"
	"net/url"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/gitrepo"
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

	// Initialize the data-client wrapper with slog-adapted logger.
	enableDataClientLogs := gitrepo.GetGitConfigBool("drs.enable-data-client-logs", false)

	logOpts := []logs.Option{
		logs.WithBaseLogger(logger),
		logs.WithNoConsole(),
	}

	if enableDataClientLogs {
		logOpts = append(logOpts, logs.WithMessageFile())
	} else {
		logOpts = append(logOpts, logs.WithNoMessageFile())
	}

	dLogger, closer := logs.New(profileConfig.Profile, logOpts...)
	_ = closer
	api := g3client.NewGen3InterfaceFromCredential(&profileConfig, dLogger, g3client.WithClients(g3client.SyfonClient))

	// Configure the DRS client with git-specific context
	upsert := gitrepo.GetGitConfigBool("drs.upsert", false)
	multiPartThresholdInt := gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)
	var multiPartThreshold int64 = int64(multiPartThresholdInt) * 1024 * 1024
	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	return &client.GitContext{
		API:                api,
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
