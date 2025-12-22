package client

import (
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
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
	GetObject(id string) (*drs.DRSObject, error)

	// list all objects available to you
	ListObjects() (chan drs.DRSObjectResult, error)

	// Given a projectId, list all of the records associated with it
	ListObjectsByProject(project string) (chan drs.DRSObjectResult, error)

	// Get a signed url given a DRS ID
	// corresponds to /ga4gh/drs/v1/objects/{object_id}/access/{access_id}
	GetDownloadURL(oid string) (*drs.AccessURL, error)

	// given a hash, get the objects describing it
	// no corresponding DRS endpoint exists, so this is custom code
	GetObjectByHash(hash *hash.Checksum) ([]drs.DRSObject, error)

	///////////////////////
	// DRS WRITE METHODS //
	///////////////////////

	// Delete all indexd records in a given project
	DeleteRecordsByProject(project string) error

	// Delete an indexd record given an oid string
	DeleteRecord(oid string) error

	// Register a DRS object directly in indexd
	RegisterRecord(indexdObject *drs.DRSObject) (*drs.DRSObject, error)

	// Put file into object storage and obtain a DRS record pointing to it
	RegisterFile(oid string) (*drs.DRSObject, error)

	// Update a DRS record and return the updated record
	// Fields allowed: URLs, authz, name, version, description
	UpdateRecord(updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error)

	// Create a DRS object given file info
	BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error)

	// Add an S3 URL to an existing indexd record
	AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...s3_utils.AddURLOption) (s3_utils.S3Meta, error)
}
