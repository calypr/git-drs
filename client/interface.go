package client

import (
	"context"

	"github.com/calypr/data-client/common"
	dataClient "github.com/calypr/data-client/g3client"
	drs "github.com/calypr/data-client/indexd/drs"
	hash "github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/git-drs/s3_utils"
)

type DRSClient interface {
	///////////////////////
	// DRS READ METHODS //
	///////////////////////

	//If the client has a notion of a current project, return its ID
	GetProjectId() string

	// Given a DRS string ID, retrieve the object describing it
	// corresponds to /ga4gh/drs/v1/objects
	GetObject(ctx context.Context, id string) (*drs.DRSObject, error)

	// list all objects available to you
	ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error)

	// Given a projectId, list all of the records associated with it
	ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error)

	// Get a signed url given a DRS ID
	// corresponds to /ga4gh/drs/v1/objects/{object_id}/access/{access_id}
	GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error)

	// given a hash, get the objects describing it
	// no corresponding DRS endpoint exists, so this is custom code
	GetObjectByHash(ctx context.Context, hash *hash.Checksum) ([]drs.DRSObject, error)

	///////////////////////
	// DRS WRITE METHODS //
	///////////////////////

	// Delete all indexd records in a given project
	DeleteRecordsByProject(ctx context.Context, project string) error

	// Delete an indexd record given an oid string
	DeleteRecord(ctx context.Context, oid string) error

	// Register a DRS object directly in indexd
	RegisterRecord(ctx context.Context, indexdObject *drs.DRSObject) (*drs.DRSObject, error)

	// Put file into object storage and obtain a DRS record pointing to it
	RegisterFile(oid string, path string, progressCallback common.ProgressCallback) (*drs.DRSObject, error)

	// Update a DRS record and return the updated record
	// Fields allowed: URLs, authz, name, version, description
	UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error)

	// Create a DRS object given file info
	BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error)

	// Add an S3 URL to an existing indexd record
	AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...s3_utils.AddURLOption) (s3_utils.S3Meta, error)

	GetBucketName() string

	// Get the underlying Gen3Interface
	GetGen3Interface() dataClient.Gen3Interface
}
