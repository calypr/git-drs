package indexd_client

// Conversion functions between DRSObject and IndexdRecord

import (
	"fmt"

	"github.com/calypr/git-drs/drs"
)

// IndexdRecord represents a simplified version of an indexd record for conversion purposes
func indexdRecordFromDrsObject(drsObj *drs.DRSObject) (*IndexdRecord, error) {
	indexdObj := &IndexdRecord{
		Did:      drsObj.Id,
		Size:     drsObj.Size,
		FileName: drsObj.Name,
		URLs:     indexdURLFromDrsAccessURLs(drsObj.AccessMethods),
		Authz:    indexdAuthzFromDrsAccessMethods(drsObj.AccessMethods),
		Hashes:   drsObj.Checksums,
		//Metadata: drsObj.Metadata,
		//Form:     drsObj.Form,
	}
	return indexdObj, nil
}

func indexdRecordToDrsObject(indexdObj *IndexdRecord) (*drs.DRSObject, error) {
	accessMethods, err := drsAccessMethodsFromIndexdURLs(indexdObj.URLs, indexdObj.Authz)
	if err != nil {
		return nil, err
	}
	for _, am := range accessMethods {
		if am.Authorizations == nil || am.Authorizations.Value == "" {
			return nil, fmt.Errorf("access method missing authorization %v, %v", indexdObj, indexdObj.Authz)
		}
	}

	return &drs.DRSObject{
		Id:   indexdObj.Did,
		Size: indexdObj.Size,
		//Form:  indexdObj.Form,
		Name:          indexdObj.FileName,
		AccessMethods: accessMethods,
		Checksums:     indexdObj.Hashes,
		//Metadata: indexdObj.Metadata,
	}, nil
}

func drsAccessMethodsFromIndexdURLs(urls []string, authz []string) ([]drs.AccessMethod, error) {
	var accessMethods []drs.AccessMethod
	for _, url := range urls {
		var method drs.AccessMethod
		method.AccessURL = drs.AccessURL{URL: url}

		// check if authz is null or 0-length, then error
		if authz == nil {
			return nil, fmt.Errorf("authz is required")
		}

		// NOTE: a record can only have 1 authz entry atm
		// this is fine since rn we're creating UUIDs based on project ID
		method.Authorizations = &drs.Authorizations{Value: authz[0]}
		accessMethods = append(accessMethods, method)
	}
	return accessMethods, nil
}

// extract authz values from DRS access methods
func indexdAuthzFromDrsAccessMethods(accessMethods []drs.AccessMethod) []string {
	var authz []string
	for _, drsURL := range accessMethods {
		if drsURL.Authorizations != nil {
			authz = append(authz, drsURL.Authorizations.Value)
		}
	}
	return authz
}

func indexdURLFromDrsAccessURLs(accessMethods []drs.AccessMethod) []string {
	var urls []string
	for _, drsURL := range accessMethods {
		urls = append(urls, drsURL.AccessURL.URL)
	}
	return urls
}

func (inr *IndexdRecord) ToDrsObject() (*drs.DRSObject, error) {
	o, err := indexdRecordToDrsObject(inr)
	if err != nil {
		return nil, err
	}
	return o, nil
}
