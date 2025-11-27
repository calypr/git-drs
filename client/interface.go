package client

import (
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/s3_utils"
)

type DRSClient interface {

	//If the client has a notion of a current project, return its ID
	GetProjectId() string

	// Given a DRS string ID, retrieve the object describing it
	// corresponds to /ga4gh/drs/v1/objects
	GetObject(id string) (*drs.DRSObject, error)

	// list all objects available to you
	ListObjects() (chan drs.DRSObjectResult, error)

	// Given a projectId, list all of the records associated with it
	ListObjectsByProject(project string) (chan drs.DRSObjectResult, error)

	// Put file into object storage and obtain a DRS record pointing to it
	// no DRS write endpoint exists, so this is custom code
	// RegisterFile(oid string) (*drs.DRSObject, error)

	// Get a signed url given a DRS ID
	// corresponds to /ga4gh/drs/v1/objects/{object_id}/access/{access_id}
	GetDownloadURL(oid string) (*drs.AccessURL, error)

	// given a hash, get the objects describing it
	// no corresponding DRS endpoint exists, so this is custom code
	GetObjectsByHash(hashType string, hash string) ([]drs.DRSObject, error)

	// Delete an indexd record given an oid string
	DeleteRecord(oid string) error

	// Register a DRS object directly in indexd
	RegisterRecord(indexdObject *drs.DRSObject) (*drs.DRSObject, error)

	// Update an indexd record by appending a file URL and return the updated record
	UpdateRecord(updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error)

	// Add an S3 URL to an existing indexd record
	AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...s3_utils.AddURLOption) (s3_utils.S3Meta, error)
}
