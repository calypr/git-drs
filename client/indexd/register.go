package indexd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"golang.org/x/sync/errgroup"
)

// RegisterFile implements DRSClient.RegisterFile
// It registers (or reuses) an indexd record for the oid, uploads the object if it
// is not already available in the bucket, and returns the resulting DRS object.
func (cl *GitDrsIdxdClient) RegisterFile(ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	oid = drs.NormalizeOid(oid)
	cl.Logger.DebugContext(ctx, fmt.Sprintf("register file started for oid: %s", oid))

	drsObject, err := cl.ensureRecordRegistered(ctx, oid, path)
	if err != nil {
		return nil, err
	}
	if err := cl.uploadFileForObject(ctx, drsObject, path, true); err != nil {
		return nil, err
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

func uploadKeyFromObject(obj *drs.DRSObject, bucket string) string {
	if obj != nil && len(obj.AccessMethods) > 0 {
		raw := strings.TrimSpace(obj.AccessMethods[0].AccessURL.URL)
		if raw != "" {
			if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Scheme, "s3") {
				if strings.TrimSpace(u.Host) == strings.TrimSpace(bucket) {
					key := strings.TrimSpace(strings.TrimPrefix(u.Path, "/"))
					if key != "" {
						return key
					}
				}
			}
		}
	}
	if obj != nil {
		return obj.Checksums.SHA256
	}
	return ""
}

func (cl *GitDrsIdxdClient) ensureRecordRegistered(ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		stat, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record for oid %s: %v (also failed to stat file %s: %v)", oid, err, path, statErr)
		}
		drsId := drsmap.DrsUUID(cl.Config.ProjectId, oid)
		drsObject, err = cl.BuildDrsObj(filepath.Base(path), oid, stat.Size(), drsId)
		if err != nil {
			return nil, fmt.Errorf("error building drs info for oid %s: %v", oid, err)
		}
	}
	cl.Logger.InfoContext(ctx, fmt.Sprintf("registering record for oid %s in indexd (did: %s)", oid, drsObject.Id))
	registeredObj, err := cl.RegisterRecord(ctx, drsObject)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			if !cl.Config.Upsert {
				cl.Logger.DebugContext(ctx, fmt.Sprintf("indexd record already exists, proceeding for oid %s: did: %s err: %v", oid, drsObject.Id, err))
				if recs, lookupErr := cl.GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid}); lookupErr == nil && len(recs) > 0 {
					if match, matchErr := drsmap.FindMatchingRecord(recs, cl.GetOrganization(), cl.Config.ProjectId); matchErr == nil && match != nil {
						drsObject = match
					}
				}
			} else {
				cl.Logger.DebugContext(ctx, fmt.Sprintf("indexd record already exists, deleting and re-adding for oid %s: did: %s", oid, drsObject.Id))
				err = cl.DeleteRecord(ctx, oid)
				if err != nil {
					return nil, fmt.Errorf("error deleting existing indexd record oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				registeredObj, err = cl.RegisterRecord(ctx, drsObject)
				if err != nil {
					return nil, fmt.Errorf("error re-saving indexd record after deletion: oid %s: did: %s err: %v", oid, drsObject.Id, err)
				}
				if registeredObj != nil {
					drsObject = registeredObj
				}
			}
		} else {
			return nil, fmt.Errorf("error saving oid %s indexd record: %v", oid, err)
		}
	} else if registeredObj != nil {
		drsObject = registeredObj
	}
	cl.Logger.InfoContext(ctx, fmt.Sprintf("indexd record registration complete for oid %s", oid))
	return drsObject, nil
}

func (cl *GitDrsIdxdClient) uploadFileForObject(ctx context.Context, drsObject *drs.DRSObject, filePath string, skipIfDownloadable bool) error {
	if skipIfDownloadable {
		cl.Logger.InfoContext(ctx, fmt.Sprintf("checking if oid %s is already downloadable", drsObject.Checksums.SHA256))
		downloadable, err := cl.isFileDownloadable(ctx, drsObject)
		if err != nil {
			return fmt.Errorf("error checking if file is downloadable: oid %s %v", drsObject.Checksums.SHA256, err)
		}
		if downloadable {
			cl.Logger.DebugContext(ctx, fmt.Sprintf("file %s is already available for download, skipping upload", drsObject.Checksums.SHA256))
			return nil
		}
	}

	cl.Logger.InfoContext(ctx, fmt.Sprintf("file %s is not downloadable, proceeding to upload", drsObject.Checksums.SHA256))
	g3 := cl.G3
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			cl.Logger.DebugContext(ctx, fmt.Sprintf("warning: error closing file %s: %v", filePath, err))
		}
	}(file)

	multiPartThreshold := int64(5 * 1024 * 1024 * 1024)
	if cl.Config.MultiPartThreshold > 0 {
		multiPartThreshold = cl.Config.MultiPartThreshold
	}
	objectKey := uploadKeyFromObject(drsObject, cl.Config.BucketName)

	if drsObject.Size < multiPartThreshold {
		cl.Logger.DebugContext(ctx, fmt.Sprintf("UploadSingle size: %d path: %s", drsObject.Size, filePath))
		req := common.FileUploadRequestObject{
			SourcePath: filePath,
			ObjectKey:  objectKey,
			GUID:       drsObject.Id,
			Bucket:     cl.Config.BucketName,
		}
		if err := upload.UploadSingle(ctx, g3, req, false); err != nil {
			return fmt.Errorf("UploadSingle error: %s", err)
		}
		return nil
	}

	cl.Logger.DebugContext(ctx, fmt.Sprintf("MultipartUpload size: %d path: %s", drsObject.Size, filePath))
	if err := upload.MultipartUpload(
		ctx,
		g3,
		common.FileUploadRequestObject{
			SourcePath:   filePath,
			ObjectKey:    objectKey,
			GUID:         drsObject.Id,
			FileMetadata: common.FileMetadata{},
			Bucket:       cl.Config.BucketName,
		},
		file, false,
	); err != nil {
		return fmt.Errorf("MultipartUpload error: %s", err)
	}
	return nil
}

// BatchSyncForPush performs checksum-first push preparation:
//  1. Bulk lookup by sha256
//  2. Bulk register missing metadata
//  3. Upload only objects that are missing/invalid in storage
func (cl *GitDrsIdxdClient) BatchSyncForPush(ctx context.Context, files map[string]lfs.LfsFileInfo) error {
	if len(files) == 0 {
		return nil
	}

	filesByOID := make(map[string]lfs.LfsFileInfo, len(files))
	oids := make([]string, 0, len(files))
	for _, f := range files {
		oid := drs.NormalizeOid(f.Oid)
		if oid == "" {
			continue
		}
		if _, exists := filesByOID[oid]; exists {
			continue
		}
		f.Oid = oid
		filesByOID[oid] = f
		oids = append(oids, oid)
	}
	sort.Strings(oids)

	existingByHash, err := cl.BatchGetObjectsByHash(ctx, oids)
	if err != nil {
		return fmt.Errorf("bulk hash lookup failed: %w", err)
	}

	validityByHash, err := cl.getSHA256ValidityMap(ctx, oids)
	if err != nil {
		cl.Logger.WarnContext(ctx, "sha256 validity probe unavailable; reusing index-only presence", "err", err)
		validityByHash = nil
	}

	drsObjByOID := make(map[string]*drs.DRSObject, len(oids))
	toRegister := make([]*drs.DRSObject, 0)
	registeredOids := make(map[string]struct{})

	for _, oid := range oids {
		file := filesByOID[oid]
		var candidate *drs.DRSObject
		if localObj, localErr := drsmap.DrsInfoFromOid(oid); localErr == nil && localObj != nil {
			candidate = localObj
		} else {
			stat, statErr := os.Stat(file.Name)
			if statErr != nil {
				return fmt.Errorf("failed to stat file %s for oid %s: %w", file.Name, oid, statErr)
			}
			did := drsmap.DrsUUID(cl.Config.ProjectId, oid)
			obj, buildErr := cl.BuildDrsObj(filepath.Base(file.Name), oid, stat.Size(), did)
			if buildErr != nil {
				return fmt.Errorf("failed to build drs object for oid %s: %w", oid, buildErr)
			}
			candidate = obj
		}
		drsObjByOID[oid] = candidate

		recs := existingByHash[oid]
		if len(recs) == 0 {
			toRegister = append(toRegister, candidate)
			registeredOids[oid] = struct{}{}
			continue
		}
		if match, matchErr := drsmap.FindMatchingRecord(recs, cl.GetOrganization(), cl.Config.ProjectId); matchErr == nil && match != nil {
			drsObjByOID[oid] = match
		}
	}

	if len(toRegister) > 0 {
		cl.Logger.InfoContext(ctx, fmt.Sprintf("bulk registering %d missing records", len(toRegister)))
		registered, regErr := cl.BatchRegisterRecords(ctx, toRegister)
		if regErr != nil {
			return fmt.Errorf("bulk register failed: %w", regErr)
		}
		for _, obj := range registered {
			if obj == nil {
				continue
			}
			oid := drs.NormalizeOid(obj.Checksums.SHA256)
			if oid != "" {
				drsObjByOID[oid] = obj
			}
		}
	}

	type uploadCandidate struct {
		oid  string
		obj  *drs.DRSObject
		file lfs.LfsFileInfo
		size int64
	}
	uploadCandidates := make([]uploadCandidate, 0, len(oids))

	for _, oid := range oids {
		exists := len(existingByHash[oid]) > 0
		_, wasMissing := registeredOids[oid]
		needsUpload := wasMissing
		if !needsUpload {
			if validityByHash == nil {
				needsUpload = !exists
			} else {
				needsUpload = !validityByHash[oid]
			}
		}
		if !needsUpload {
			continue
		}
		obj := drsObjByOID[oid]
		if obj == nil {
			return fmt.Errorf("missing drs object context for oid %s", oid)
		}
		file := filesByOID[oid]
		size := file.Size
		if size <= 0 {
			if stat, statErr := os.Stat(file.Name); statErr == nil {
				size = stat.Size()
			} else {
				return fmt.Errorf("failed to stat file %s for oid %s: %w", file.Name, oid, statErr)
			}
		}
		uploadCandidates = append(uploadCandidates, uploadCandidate{
			oid:  oid,
			obj:  obj,
			file: file,
			size: size,
		})
	}

	if len(uploadCandidates) == 0 {
		return nil
	}

	threshold := int64(5 * 1024 * 1024 * 1024)
	if cl.Config != nil && cl.Config.MultiPartThreshold > 0 {
		threshold = cl.Config.MultiPartThreshold
	}
	concurrency := 1
	if cl.Config != nil && cl.Config.UploadConcurrency > 0 {
		concurrency = cl.Config.UploadConcurrency
	}

	smallCandidates := make([]uploadCandidate, 0, len(uploadCandidates))
	largeCandidates := make([]uploadCandidate, 0, len(uploadCandidates))
	for _, candidate := range uploadCandidates {
		if candidate.size < threshold {
			smallCandidates = append(smallCandidates, candidate)
		} else {
			largeCandidates = append(largeCandidates, candidate)
		}
	}

	cl.Logger.InfoContext(ctx, "upload plan prepared",
		"total", len(uploadCandidates),
		"small_singlepart_parallel", len(smallCandidates),
		"large_multipart_sequential", len(largeCandidates),
		"upload_concurrency", concurrency,
		"multipart_threshold_bytes", threshold,
	)

	if len(smallCandidates) > 0 {
		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(concurrency)
		for _, candidate := range smallCandidates {
			candidate := candidate
			eg.Go(func() error {
				if err := cl.uploadFileForObject(egCtx, candidate.obj, candidate.file.Name, false); err != nil {
					return fmt.Errorf("upload failed for %s (%s): %w", candidate.file.Name, candidate.oid, err)
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	for _, candidate := range largeCandidates {
		if err := cl.uploadFileForObject(ctx, candidate.obj, candidate.file.Name, false); err != nil {
			return fmt.Errorf("upload failed for %s (%s): %w", candidate.file.Name, candidate.oid, err)
		}
	}

	return nil
}

func (cl *GitDrsIdxdClient) getSHA256ValidityMap(ctx context.Context, oids []string) (map[string]bool, error) {
	cred := cl.G3.GetCredential()
	if cred == nil || strings.TrimSpace(cred.APIEndpoint) == "" {
		return nil, fmt.Errorf("missing API endpoint for validity check")
	}

	reqBody, err := json.Marshal(map[string][]string{"sha256": oids})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cred.APIEndpoint, "/")+"/index/bulk/sha256/validity", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(cred.AccessToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("validity endpoint status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
