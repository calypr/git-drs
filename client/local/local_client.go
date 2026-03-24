package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/calypr/data-client/backend"
	drs_backend "github.com/calypr/data-client/backend/drs"
	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/download"
	drs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/data-client/request"
	"github.com/calypr/data-client/s3utils"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/lfs"
	"golang.org/x/sync/errgroup"
)

type LocalRemote struct {
	BaseURL       string
	ProjectID     string
	Bucket        string
	Organization  string
	StoragePrefix string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}

func (l LocalRemote) GetOrganization() string {
	return l.Organization
}

func (l LocalRemote) GetEndpoint() string {
	return l.BaseURL
}

func (l LocalRemote) GetBucketName() string {
	return l.Bucket
}

func (l LocalRemote) GetStoragePrefix() string {
	return l.StoragePrefix
}

func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	return NewLocalClient(l, logger), nil
}

type LocalClient struct {
	Remote  LocalRemote
	Logger  *slog.Logger
	Backend backend.Backend
	Config  *LocalConfig
}

type LocalConfig struct {
	MultiPartThreshold int64
	UploadConcurrency  int
}

func NewLocalClient(remote LocalRemote, logger *slog.Logger) *LocalClient {
	// Initialize RequestInterface for DrsBackend
	cred := &conf.Credential{
		APIEndpoint: remote.BaseURL,
	}
	gen3Logger := logs.NewGen3Logger(logger, "", "")
	cfg := conf.NewConfigure(logger)
	req := request.NewRequestInterface(gen3Logger, cred, cfg)

	// Initialize the DRS backend
	bk := drs_backend.NewDrsBackend(remote.BaseURL, logger, req)
	multiPartThresholdInt := gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)
	multiPartThreshold := int64(multiPartThresholdInt) * common.MB
	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	return &LocalClient{
		Remote:  remote,
		Logger:  logger,
		Backend: bk,
		Config: &LocalConfig{
			MultiPartThreshold: multiPartThreshold,
			UploadConcurrency:  uploadConcurrency,
		},
	}
}

// Helpers

// Helpers removed as they are replaced by data-client functionality

// Implement DRSClient interface

func (c *LocalClient) GetProjectId() string {
	return c.Remote.GetProjectId()
}

func (c *LocalClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return c.Backend.GetFileDetails(ctx, id)
}

func (c *LocalClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return nil, fmt.Errorf("ListObjects not implemented for LocalClient")
}

func (c *LocalClient) ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error) {
	return nil, fmt.Errorf("ListObjectsByProject not implemented for LocalClient")
}

func (c *LocalClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	// 1. Get Object to find access method
	obj, err := c.GetObject(ctx, oid)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", oid, err)
	}

	if len(obj.AccessMethods) == 0 {
		return nil, fmt.Errorf("no access methods found for object %s", oid)
	}

	// 2. Naively pick specific access method if we knew it, or just first one?
	// git-drs/client/indexd/client.go picks first one or tries to match accessType.
	// We'll pick the first one that has an access_id (indicating we need to call /access endpoint)
	// or matches a type we like (e.g. 's3', 'gs', 'http', 'https').
	// If AccessURL.URL is already present in AccessMethod (inline), use it.

	var accessID string

	// Prefer file://, then HTTP/HTTPS, then S3
	for _, am := range obj.AccessMethods {
		if strings.HasPrefix(am.AccessURL.URL, "file://") {
			return &am.AccessURL, nil
		}
	}

	for _, am := range obj.AccessMethods {
		if am.AccessURL.URL != "" {
			// Direct URL available
			return &am.AccessURL, nil
		}
		// If we have an AccessID, we can resolve it
		if am.AccessID != "" {
			accessID = am.AccessID
			break
		}
	}

	if accessID == "" {
		// Fallback to first if defined
		if len(obj.AccessMethods) > 0 && obj.AccessMethods[0].AccessID != "" {
			accessID = obj.AccessMethods[0].AccessID
		} else {
			return nil, fmt.Errorf("no suitable access method found for object %s", oid)
		}
	}

	// 3. Call /access endpoint using Backend
	url, err := c.Backend.GetDownloadURL(ctx, oid, accessID)
	if err != nil {
		return nil, err
	}
	return &drs.AccessURL{URL: url}, nil
}

func (c *LocalClient) GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error) {
	res, err := c.Backend.GetObjectByHash(ctx, string(checksum.Type), checksum.Checksum)
	if err != nil {
		return nil, err
	}

	filtered := make([]drs.DRSObject, 0)
	for _, o := range res {
		matched, err := drsmap.FindMatchingRecord([]drs.DRSObject{o}, c.Remote.Organization, c.Remote.GetProjectId())
		if err == nil && matched != nil {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
}

func (c *LocalClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return c.Backend.BatchGetObjectsByHash(ctx, hashes)
}

func (c *LocalClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	// Not supported by Backend interface yet, keeping as no-op or we can implement in backend if needed
	return fmt.Errorf("Not Implemented")
}

func (c *LocalClient) DeleteRecord(ctx context.Context, oid string) error {
	return fmt.Errorf("Not Implemented")
}

// RegisterRecord registers a DRS object with the server
func (c *LocalClient) RegisterRecord(ctx context.Context, indexdObject *drs.DRSObject) (*drs.DRSObject, error) {
	return c.Backend.Register(ctx, indexdObject)
}

func (c *LocalClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return c.Backend.BatchRegister(ctx, records)
}

func (c *LocalClient) RegisterFile(ctx context.Context, oid string, filePath string) (*drs.DRSObject, error) {
	oid = drs.NormalizeOid(oid)
	drsObject, err := c.loadOrBuildDrsObject(oid, filePath)
	if err != nil {
		return nil, err
	}

	c.Logger.InfoContext(ctx, fmt.Sprintf("registering record for oid %s in DRS server (did: %s)", oid, drsObject.Id))
	registeredObj, err := c.RegisterRecord(ctx, drsObject)
	if err != nil {
		return nil, fmt.Errorf("error registering record: %v", err)
	}
	drsObject = registeredObj
	if err := c.uploadFileForObject(ctx, filePath, drsObject); err != nil {
		return nil, err
	}
	return drsObject, nil
}

func (c *LocalClient) loadOrBuildDrsObject(oid string, filePath string) (*drs.DRSObject, error) {
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err == nil && drsObject != nil {
		return drsObject, nil
	}
	stat, statErr := os.Stat(filePath)
	if statErr != nil {
		return nil, fmt.Errorf("error reading local record: %v", statErr)
	}
	drsId := drsmap.DrsUUID(c.Remote.GetProjectId(), oid)
	return c.BuildDrsObj(filepath.Base(filePath), oid, stat.Size(), drsId)
}

func (c *LocalClient) uploadFileForObject(ctx context.Context, filePath string, drsObject *drs.DRSObject) error {
	if c.Remote.GetBucketName() == "" {
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	threshold := int64(500 * common.MB)
	if c.Config != nil && c.Config.MultiPartThreshold > 0 {
		threshold = c.Config.MultiPartThreshold
	}

	if drsObject.Size < threshold {
		uploadURL, err := c.Backend.GetUploadURL(ctx, drsObject.Id, "", common.FileMetadata{}, c.Remote.GetBucketName())
		if err != nil {
			return fmt.Errorf("failed to get signed upload URL: %w", err)
		}
		c.Logger.DebugContext(ctx, "performing single-part upload", "url", uploadURL)
		return c.Backend.Upload(ctx, uploadURL, file, drsObject.Size)
	}

	c.Logger.DebugContext(ctx, "performing multipart upload", "size", drsObject.Size, "threshold", threshold)
	return c.uploadMultipart(ctx, file, drsObject)
}

// BatchSyncForPush performs checksum-first push preparation:
//  1. Bulk lookup by sha256
//  2. Bulk register missing metadata
//  3. Upload only objects that are missing/invalid in storage
func (c *LocalClient) BatchSyncForPush(ctx context.Context, files map[string]lfs.LfsFileInfo) error {
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

	existingByHash, err := c.BatchGetObjectsByHash(ctx, oids)
	if err != nil {
		return fmt.Errorf("bulk hash lookup failed: %w", err)
	}

	validityByHash, err := c.getSHA256ValidityMap(ctx, oids)
	if err != nil {
		c.Logger.WarnContext(ctx, "sha256 validity probe unavailable; reusing index-only presence", "err", err)
		validityByHash = nil
	}

	drsObjByOID := make(map[string]*drs.DRSObject, len(oids))
	toRegister := make([]*drs.DRSObject, 0)
	registeredOids := make(map[string]struct{})

	for _, oid := range oids {
		file := filesByOID[oid]
		obj, loadErr := c.loadOrBuildDrsObject(oid, file.Name)
		if loadErr != nil {
			return loadErr
		}
		drsObjByOID[oid] = obj
		if len(existingByHash[oid]) == 0 {
			toRegister = append(toRegister, obj)
			registeredOids[oid] = struct{}{}
		}
	}

	if len(toRegister) > 0 {
		c.Logger.InfoContext(ctx, fmt.Sprintf("bulk registering %d missing records", len(toRegister)))
		registered, regErr := c.BatchRegisterRecords(ctx, toRegister)
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
	if c.Config != nil && c.Config.MultiPartThreshold > 0 {
		threshold = c.Config.MultiPartThreshold
	}
	concurrency := 1
	if c.Config != nil && c.Config.UploadConcurrency > 0 {
		concurrency = c.Config.UploadConcurrency
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

	c.Logger.InfoContext(ctx, "upload plan prepared",
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
				if err := c.uploadFileForObject(egCtx, candidate.file.Name, candidate.obj); err != nil {
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
		if err := c.uploadFileForObject(ctx, candidate.file.Name, candidate.obj); err != nil {
			return fmt.Errorf("upload failed for %s (%s): %w", candidate.file.Name, candidate.oid, err)
		}
	}

	return nil
}

func (c *LocalClient) getSHA256ValidityMap(ctx context.Context, oids []string) (map[string]bool, error) {
	base := strings.TrimRight(strings.TrimSpace(c.Remote.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("missing local endpoint for validity check")
	}
	reqBody, err := json.Marshal(map[string][]string{"sha256": oids})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/index/bulk/sha256/validity", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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

func (c *LocalClient) uploadMultipart(ctx context.Context, file *os.File, drsObject *drs.DRSObject) error {
	// Canonical key is server-owned; multipart session resolves it server-side from GUID.
	initResp, err := c.Backend.InitMultipartUpload(ctx, drsObject.Id, "", c.Remote.GetBucketName())
	if err != nil {
		return fmt.Errorf("failed to init multipart upload: %w", err)
	}
	if initResp == nil || initResp.UploadID == "" {
		return fmt.Errorf("multipart init returned empty uploadId")
	}

	chunkSize := upload.OptimalChunkSize(drsObject.Size)
	numChunks := int((drsObject.Size + chunkSize - 1) / chunkSize)
	parts := make([]common.MultipartUploadPart, numChunks)
	concurrency := 4
	if c.Config != nil && c.Config.UploadConcurrency > 0 {
		concurrency = c.Config.UploadConcurrency
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(concurrency)

	for partNum := 1; partNum <= numChunks; partNum++ {
		partNum := partNum
		eg.Go(func() error {
			offset := int64(partNum-1) * chunkSize
			size := chunkSize
			if offset+size > drsObject.Size {
				size = drsObject.Size - offset
			}

			partURL, err := c.Backend.GetMultipartUploadURL(egCtx, "", initResp.UploadID, int32(partNum), c.Remote.GetBucketName())
			if err != nil {
				return fmt.Errorf("failed to get signed url for part %d: %w", partNum, err)
			}

			section := io.NewSectionReader(file, offset, size)
			etag, err := c.Backend.UploadPart(egCtx, partURL, section, size)
			if err != nil {
				return fmt.Errorf("failed uploading multipart part %d: %w", partNum, err)
			}

			parts[partNum-1] = common.MultipartUploadPart{
				PartNumber: int32(partNum),
				ETag:       etag,
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	for i := range parts {
		if parts[i].PartNumber == 0 || parts[i].ETag == "" {
			return fmt.Errorf("multipart upload incomplete: missing metadata for part index %d", i)
		}
	}

	if err := c.Backend.CompleteMultipartUpload(ctx, "", initResp.UploadID, parts, c.Remote.GetBucketName()); err != nil {
		return fmt.Errorf("failed completing multipart upload: %w", err)
	}
	return nil
}

func (c *LocalClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	// Backend interface doesn't support UpdateRecord yet.
	return nil, fmt.Errorf("UpdateRecord not implemented for LocalClient")
}

func (c *LocalClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	builder := drs.NewObjectBuilder(c.Remote.GetBucketName(), c.Remote.GetProjectId())
	builder.Organization = c.Remote.Organization
	builder.StoragePrefix = c.Remote.StoragePrefix
	builder.PathStyle = "CAS"
	return builder.Build(fileName, checksum, size, drsId)
}

func (c *LocalClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...cloud.AddURLOption) (s3utils.S3Meta, error) {
	return s3utils.S3Meta{}, fmt.Errorf("AddURL not implemented for LocalClient")
}

func (c *LocalClient) GetBucketName() string {
	return c.Remote.GetBucketName()
}

func (c *LocalClient) GetOrganization() string {
	return c.Remote.Organization
}

func (c *LocalClient) GetGen3Interface() g3client.Gen3Interface {
	return nil
}

func (c *LocalClient) DownloadFile(ctx context.Context, guid string, destPath string) error {
	opts := download.DownloadOptions{
		MultipartThreshold: int64(5 * common.GB),
	}
	if c.Config != nil && c.Config.MultiPartThreshold > 0 {
		opts.MultipartThreshold = c.Config.MultiPartThreshold
	}
	return download.DownloadToPathWithOptions(ctx, c.Backend, c.Logger, guid, destPath, "", opts)
}
