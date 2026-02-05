package indexd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/drsmap"
)

// RegisterFile implements DRSClient.RegisterFile
// It registers (or reuses) an indexd record for the oid, uploads the object if it
// is not already available in the bucket, and returns the resulting DRS object.
func (cl *GitDrsIdxdClient) RegisterFile(ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	cl.Logger.DebugContext(ctx, fmt.Sprintf("register file started for oid: %s", oid))

	// load the DRS object from oid created by prepush
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		return nil, fmt.Errorf("error getting drs object for oid %s: %v", oid, err)
	}

	cl.Logger.InfoContext(ctx, fmt.Sprintf("registering record for oid %s in indexd (did: %s)", oid, drsObject.Id))
	_, err = cl.RegisterRecord(ctx, drsObject)
	if err != nil {
		// handle "already exists" error ie upsert behavior
		if strings.Contains(err.Error(), "already exists") {
			if !cl.Config.Upsert {
				cl.Logger.DebugContext(ctx, fmt.Sprintf("indexd record already exists, proceeding for oid %s: did: %s err: %v", oid, drsObject.Id, err))
			} else {
				cl.Logger.DebugContext(ctx, fmt.Sprintf("indexd record already exists, deleting and re-adding for oid %s: did: %s", oid, drsObject.Id))
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
	cl.Logger.InfoContext(ctx, fmt.Sprintf("indexd record registration complete for oid %s", oid))

	// Now attempt to upload the file if not already available
	cl.Logger.InfoContext(ctx, fmt.Sprintf("checking if oid %s is already downloadable", oid))
	downloadable, err := cl.isFileDownloadable(ctx, drsObject)
	if err != nil {
		return nil, fmt.Errorf("error checking if file is downloadable: oid %s %v", oid, err)
	}
	if downloadable {
		cl.Logger.DebugContext(ctx, fmt.Sprintf("file %s is already available for download, skipping upload", oid))
		return drsObject, nil
	}
	cl.Logger.InfoContext(ctx, fmt.Sprintf("file %s is not downloadable, proceeding to upload", oid))

	// Proceed to upload the file
	// Reuse the Gen3 interface
	g3 := cl.G3

	filePath := path
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			cl.Logger.DebugContext(ctx, fmt.Sprintf("warning: error closing file %s: %v", filePath, err))
		}
	}(file)

	// Use multipart threshold from config or default to 5GB
	multiPartThreshold := int64(5 * 1024 * 1024 * 1024) // 5GB default
	if cl.Config.MultiPartThreshold > 0 {
		multiPartThreshold = cl.Config.MultiPartThreshold
	}

	if drsObject.Size < multiPartThreshold {
		cl.Logger.DebugContext(ctx, fmt.Sprintf("UploadSingle size: %d path: %s", drsObject.Size, filePath))
		req := common.FileUploadRequestObject{
			SourcePath: filePath,
			ObjectKey:  drsObject.Checksums.SHA256,
			GUID:       drsObject.Id,
			Bucket:     cl.Config.BucketName,
		}
		err := upload.UploadSingle(ctx, g3, req, false)
		if err != nil {
			return nil, fmt.Errorf("UploadSingle error: %s", err)
		}
	} else {
		cl.Logger.DebugContext(ctx, fmt.Sprintf("MultipartUpload size: %d path: %s", drsObject.Size, filePath))
		err = upload.MultipartUpload(
			ctx,
			g3,
			common.FileUploadRequestObject{
				SourcePath:   filePath,
				ObjectKey:    drsObject.Checksums.SHA256,
				GUID:         drsObject.Id,
				FileMetadata: common.FileMetadata{},
				Bucket:       cl.Config.BucketName,
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
func (cl *GitDrsIdxdClient) isFileDownloadable(ctx context.Context, drsObject *drs.DRSObject) (bool, error) {
	// Try to get a download URL - if successful, file is downloadable
	if len(drsObject.AccessMethods) == 0 {
		return false, nil
	}
	accessType := drsObject.AccessMethods[0].Type
	res, err := cl.G3.Indexd().GetDownloadURL(ctx, drsObject.Id, accessType)
	if err != nil {
		// If we can't get a download URL, assume file is not downloadable
		return false, nil
	}
	// Check if the URL is accessible
	err = common.CanDownloadFile(res.URL)
	return err == nil, nil
}
