package client

import "github.com/bmeg/git-drs/drs"

type ObjectStoreClient interface {
	//Given a DRS string ID, retrieve the object describing it
	QueryID(id string) (*drs.DRSObject, error)

	//Put file into object storage and obtain a DRS record pointing to it
	RegisterFile(oid string) (*drs.DRSObject, error)

	//Download file given a DRS ID
	DownloadFile(id string, access_id string, dstPath string) (*drs.AccessURL, error)
}
