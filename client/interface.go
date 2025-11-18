package client

import "github.com/calypr/git-drs/drs"

type ObjectStoreClient interface {
	// Given a DRS string ID, retrieve the object describing it
	// corresponds to /ga4gh/drs/v1/objects
	GetObject(id string) (*drs.DRSObject, error)

	// list all objects available to you
	ListObjects() (chan drs.DRSObjectResult, error)

	// Given a projectId, list all of the records associated with it
	ListObjectsByProject(project string) (chan ListRecordsResult, error)

	// Put file into object storage and obtain a DRS record pointing to it
	// no DRS write endpoint exists, so this is custom code
	RegisterFile(oid string, path string) (*drs.DRSObject, error)

	// Get a signed url given a DRS ID
	// corresponds to /ga4gh/drs/v1/objects/{object_id}/access/{access_id}
	GetDownloadURL(oid string) (*drs.AccessURL, error)

	// For issue #45: Split out Object store into DRS and Indexd Clients

	// given a hash, get the objects describing it
	// no corresponding DRS endpoint exists, so this is custom code
	GetObjectsByHash(hashType string, hash string) ([]OutputInfo, error)

	// Delete an indexd record given an oid string
	// corresponds to DELETE /index/index/{guid}
	DeleteIndexdRecord(oid string) error

	// Register a DRS object directly in indexd
	RegisterIndexdRecord(indexdObject *IndexdRecord) (*drs.DRSObject, error)

	// Get an indexd record by its DID
	// corresponds to GET /index/index/{guid}
	getIndexdRecordByDID(did string) (*OutputInfo, error)

	// Update an indexd record by appending a file URL and return the updated record
	// corresponds to PUT /index/index/{guid}
	UpdateIndexdRecord(updateInfo *UpdateInputInfo, did string) (*drs.DRSObject, error)
}
