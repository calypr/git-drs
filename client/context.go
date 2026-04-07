package client

import (
	"log/slog"

	"github.com/calypr/syfon/client/conf"
	"github.com/calypr/syfon/client/drs"
)

// GitContext is the lightweight composition root grouping the operational capability (API)
// with the target environment layout strings (Project, Bucket, Organization).
// It replaces the legacy DRSClient god-object wrapper.
type GitContext struct {
	API                drs.Client
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
