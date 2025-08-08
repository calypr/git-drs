package client

import "github.com/calypr/git-drs/drs"

type ObjectStoreClient interface {
	// Given a DRS string ID, retrieve the object describing it
	// corresponds to /ga4gh/drs/v1/objects
	GetObject(id string) (*drs.DRSObject, error)

	ListDrsObjects() (chan drs.DRSObjectResult, error)

	// Given a projectId, list all of the records associated with it
	ListObjectsByProject(project string) (chan ListRecordsResult, error)

	// given a hash, get the object describing it
	// no corresponding DRS endpoint exists, so this is custom code
	GetObjectByHash(hashType string, hash string) (*drs.DRSObject, error)

	// Put file into object storage and obtain a DRS record pointing to it
	// no DRS write endpoint exists, so this is custom code
	RegisterFile(oid string) (*drs.DRSObject, error)

	// Get a signed url given a DRS ID
	// corresponds to /ga4gh/drs/v1/objects/{object_id}/access/{access_id}
	GetDownloadURL(oid string) (*drs.AccessURL, error)

	// Delete an indexd record given an oid string
	DeleteIndexdRecord(oid string) error
}
