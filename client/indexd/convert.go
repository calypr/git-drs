package indexd_client

// Conversion functions between DRSObject and IndexdRecord

import (
	"github.com/calypr/git-drs/drs"
)

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

func indexdRecordToDrsObject(indexdObj *IndexdRecord) *drs.DRSObject {
	return &drs.DRSObject{
		Id:   indexdObj.Did,
		Size: indexdObj.Size,
		//Form:     indexdObj.Form,
		Name:          indexdObj.FileName,
		AccessMethods: drsAccessMethodsFromIndexdURLs(indexdObj.URLs, indexdObj.Authz),
		Checksums:     indexdObj.Hashes,
		//Metadata: indexdObj.Metadata,
	}
}

func drsAccessMethodsFromIndexdURLs(urls []string, authz []string) []drs.AccessMethod {
	var accessMethods []drs.AccessMethod
	for _, url := range urls {
		var method drs.AccessMethod
		method.AccessURL = drs.AccessURL{URL: url}
		// NOTE: a record can only have 1 authz entry atm
		// this is fine if we're creating UUIDs based on project ID
		method.Authorizations = &drs.Authorizations{Value: authz[0]}
		accessMethods = append(accessMethods, method)
	}
	return accessMethods
}

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

func (inr *IndexdRecord) ToDrsObject() *drs.DRSObject {
	return indexdRecordToDrsObject(inr)
}
