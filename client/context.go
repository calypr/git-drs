package client

import (
	"log/slog"

	"github.com/calypr/data-client/conf"
	syrequest "github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/syfonclient"
)

// GitContext is the lightweight composition root grouping the operational capability (API)
// with the target environment layout strings (Project, Bucket, Organization).
// It replaces the legacy DRSClient god-object wrapper.
type GitContext struct {
	Client             syfonclient.SyfonClient
	Requestor          syrequest.Requester
	Organization       string
	ProjectId          string
	BucketName         string
	StoragePrefix      string
	Upsert             bool
	MultiPartThreshold int64
	UploadConcurrency  int
	Logger             *slog.Logger
	Credential         *conf.Credential
}
