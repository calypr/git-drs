package drs

import (
	"fmt"
	"path/filepath"

	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/utils"
)

type ObjectBuilder struct {
	Bucket     string
	ProjectID  string
	AccessType string
}

func NewObjectBuilder(bucket, projectID string) ObjectBuilder {
	return ObjectBuilder{
		Bucket:     bucket,
		ProjectID:  projectID,
		AccessType: "s3",
	}
}

func (b ObjectBuilder) Build(fileName string, checksum string, size int64, drsID string) (*DRSObject, error) {
	if b.Bucket == "" {
		return nil, fmt.Errorf("error: bucket name is empty in config file")
	}
	accessType := b.AccessType
	if accessType == "" {
		accessType = "s3"
	}

	fileURL := fmt.Sprintf("s3://%s", filepath.Join(b.Bucket, drsID, checksum))

	authzStr, err := utils.ProjectToResource(b.ProjectID)
	if err != nil {
		return nil, err
	}
	authorizations := Authorizations{
		Value: authzStr,
	}

	drsObj := DRSObject{
		Id:   drsID,
		Name: fileName,
		AccessMethods: []AccessMethod{{
			Type:           accessType,
			AccessURL:      AccessURL{URL: fileURL},
			Authorizations: &authorizations,
		}},
		Checksums: hash.HashInfo{SHA256: checksum},
		Size:      size,
	}

	return &drsObj, nil
}
