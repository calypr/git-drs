package client

import (
	"log/slog"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
)

// GitContext is the lightweight composition root grouping the operational capability (API)
// with the target environment layout strings (Project, Bucket, Organization).
// It replaces the legacy DRSClient god-object wrapper.
type GitContext struct {
	API                g3client.Gen3Interface
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
