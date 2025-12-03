package indexd_client

// Conversion functions between DRSObject and IndexdRecord

import "github.com/calypr/git-drs/drs"

func indexdRecordFromDrsObject(drsObj *drs.DRSObject) (*IndexdRecord, error) {
	indexdObj := &IndexdRecord{
		Did:      drsObj.Id,
		Size:     drsObj.Size,
		FileName: drsObj.Name,
		URLs:     indexdURLFromDrsAccessURLs(drsObj.AccessMethods),
		Authz:    indexdAuthzFromDrsAccessMethods(drsObj.AccessMethods),
		Hashes:   indexdHashListFromDrsChecksums(drsObj.Checksums),
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
		Checksums:     drsChecksumFromIndexdHashInfo(indexdObj.Hashes),
		//Metadata: indexdObj.Metadata,
	}
}

func drsAccessMethodsFromIndexdURLs(urls []string, authz []string) []drs.AccessMethod {
	var accessMethods []drs.AccessMethod
	for i, url := range urls {
		var method drs.AccessMethod
		method.AccessURL = drs.AccessURL{URL: url}
		if i < len(authz) {
			method.Authorizations = &drs.Authorizations{Value: authz[i]}
		}
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

func indexdHashListFromDrsChecksums(drsChecksums []drs.Checksum) HashInfo {
	var hashInfo HashInfo
	for _, drsChecksum := range drsChecksums {
		hashInfo = indexdHashInfoFromDrsHashInfo(drsChecksum)
	}
	return hashInfo
}

func drsChecksumFromIndexdHashInfo(hashInfo HashInfo) []drs.Checksum {
	out := []drs.Checksum{}
	if hashInfo.MD5 != "" {
		out = append(out, drs.Checksum{Type: drs.ChecksumTypeMD5, Checksum: hashInfo.MD5})
	} else if hashInfo.SHA != "" {
		out = append(out, drs.Checksum{Type: drs.ChecksumTypeSHA1, Checksum: hashInfo.SHA})
	} else if hashInfo.SHA256 != "" {
		out = append(out, drs.Checksum{Type: drs.ChecksumTypeSHA256, Checksum: hashInfo.SHA256})
	} else if hashInfo.SHA512 != "" {
		out = append(out, drs.Checksum{Type: drs.ChecksumTypeSHA512, Checksum: hashInfo.SHA512})
	} else if hashInfo.CRC != "" {
		out = append(out, drs.Checksum{Type: drs.ChecksumTypeCRC32C, Checksum: hashInfo.CRC})
	} else if hashInfo.ETag != "" {
		out = append(out, drs.Checksum{Type: drs.ChecksumTypeETag, Checksum: hashInfo.ETag})
	}
	return out
}

func indexdHashInfoFromDrsHashInfo(drsChecksum drs.Checksum) HashInfo {
	switch drsChecksum.Type {
	case drs.ChecksumTypeMD5:
		return HashInfo{MD5: drsChecksum.Checksum}
	case drs.ChecksumTypeSHA1:
		return HashInfo{SHA: drsChecksum.Checksum}
	case drs.ChecksumTypeSHA256:
		return HashInfo{SHA256: drsChecksum.Checksum}
	case drs.ChecksumTypeSHA512:
		return HashInfo{SHA512: drsChecksum.Checksum}
	case drs.ChecksumTypeCRC32C:
		return HashInfo{CRC: drsChecksum.Checksum}
	case drs.ChecksumTypeETag:
		return HashInfo{ETag: drsChecksum.Checksum}
	default:
		return HashInfo{}
	}
}

func (inr *IndexdRecord) ToDrsObject() *drs.DRSObject {
	return indexdRecordToDrsObject(inr)
}
