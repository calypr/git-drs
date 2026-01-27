package indexd_client

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/common"
	drs "github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/projectdir"
)

// RegisterFile implements DRSClient.RegisterFile
// It registers (or reuses) an indexd record for the oid, uploads the object if it
// is not already available in the bucket, and returns the resulting DRS object.
func (cl *GitDrsIdxdClient) RegisterFile(oid string, progressCallback common.ProgressCallback) (*drs.DRSObject, error) {
	cl.Logger.Debug(fmt.Sprintf("register file started for oid: %s", oid))

	// load the DRS object from oid created by prepush
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		return nil, fmt.Errorf("error getting drs object for oid %s: %v", oid, err)
	}

	// Register the indexd record
	ctx := context.Background()
	_, err = cl.RegisterRecord(ctx, drsObject)
	if err != nil {
		// handle "already exists" error ie upsert behavior
		if strings.Contains(err.Error(), "already exists") {
			if !cl.Config.Upsert {
				cl.Logger.Debug(fmt.Sprintf("indexd record already exists, proceeding for oid %s: did: %s err: %v", oid, drsObject.Id, err))
			} else {
				cl.Logger.Debug(fmt.Sprintf("indexd record already exists, deleting and re-adding for oid %s: did: %s err: %v", oid, drsObject.Id, err))
				err = cl.DeleteRecord(ctx, oid)
				if err != nil {
					return nil, fmt.Errorf("error deleting existing indexd record oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				_, err = cl.RegisterRecord(ctx, drsObject)
				if err != nil {
					return nil, fmt.Errorf("error re-saving indexd record after deletion: oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
			}
		} else {
			return nil, fmt.Errorf("error saving oid %s indexd record: %v", oid, err)
		}
	}

	// Now attempt to upload the file if not already available
	downloadable, err := cl.isFileDownloadable(drsObject)
	if err != nil {
		return nil, fmt.Errorf("error checking if file is downloadable: oid %s %v", oid, err)
	}
	if downloadable {
		cl.Logger.Debug(fmt.Sprintf("file %s is already available for download, skipping upload", oid))
		return drsObject, nil
	}

	// Proceed to upload the file
	profile := cl.G3.GetCredential().Profile

	// Reuse the Gen3 interface
	g3 := cl.G3

	filePath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, oid)
	if err != nil {
		return nil, fmt.Errorf("error getting object path for oid %s: %v", oid, err)
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			cl.Logger.Debug(fmt.Sprintf("warning: error closing file %s: %v", filePath, err))
		}
	}(file)

	// Use multipart threshold from config or default to 5GB
	multiPartThreshold := int64(5 * 1024 * 1024 * 1024) // 5GB default
	if cl.Config.MultiPartThreshold > 0 {
		multiPartThreshold = cl.Config.MultiPartThreshold
	}

	if drsObject.Size < multiPartThreshold {
		cl.Logger.Debug(fmt.Sprintf("UploadSingle size: %d path: %s", drsObject.Size, filePath))
		err := upload.UploadSingle(context.Background(), profile, drsObject.Id, drsObject.Checksums.SHA256, filePath, cl.Config.BucketName, false, progressCallback)
		if err != nil {
			return nil, fmt.Errorf("UploadSingle error: %s", err)
		}
	} else {
		cl.Logger.Debug(fmt.Sprintf("MultipartUpload size: %d path: %s", drsObject.Size, filePath))
		err = upload.MultipartUpload(
			context.TODO(),
			g3,
			common.FileUploadRequestObject{
				FilePath:     filePath,
				Filename:     filepath.Base(filePath),
				GUID:         drsObject.Id,
				OID:          drsObject.Checksums.SHA256,
				FileMetadata: common.FileMetadata{},
				Bucket:       cl.Config.BucketName,
				Progress:     progressCallback,
			},
			file, false,
		)
		if err != nil {
			return nil, fmt.Errorf("MultipartUpload error: %s", err)
		}
	}
	return drsObject, nil
}

// isFileDownloadable checks if a file is already available for download
func (cl *GitDrsIdxdClient) isFileDownloadable(drsObject *drs.DRSObject) (bool, error) {
	// Try to get a download URL - if successful, file is downloadable
	if len(drsObject.AccessMethods) == 0 {
		return false, nil
	}
	accessType := drsObject.AccessMethods[0].Type
	_, err := cl.G3.Indexd().GetDownloadURL(context.Background(), drsObject.Id, accessType)
	if err != nil {
		// If we can't get a download URL, assume file is not downloadable
		return false, nil
	}
	return true, nil
}
