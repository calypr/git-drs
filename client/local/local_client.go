package local

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/download"
	drs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/data-client/request"
	"github.com/calypr/data-client/transfer"
	localsigner "github.com/calypr/data-client/transfer/signer/local"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/lfs"
	"github.com/hashicorp/go-retryablehttp"
)

type LocalRemote struct {
	BaseURL       string
	ProjectID     string
	Bucket        string
	Organization  string
	StoragePrefix string
	BasicUsername string
	BasicPassword string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}
func (l LocalRemote) GetOrganization() string { return l.Organization }
func (l LocalRemote) GetEndpoint() string     { return l.BaseURL }
func (l LocalRemote) GetBucketName() string   { return l.Bucket }
func (l LocalRemote) GetStoragePrefix() string {
	return l.StoragePrefix
}
func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	if username, password, err := gitrepo.GetRemoteBasicAuth(remoteName); err == nil && username != "" && password != "" {
		l.BasicUsername = username
		l.BasicPassword = password
	}
	return NewLocalClient(l, logger), nil
}

type LocalConfig struct {
	MultiPartThreshold int64
	UploadConcurrency  int
}

type LocalClient struct {
	Remote     LocalRemote
	Logger     *slog.Logger
	DataLogger *logs.Gen3Logger
	meta       metadataStore
	uploads    uploadService
	downloads  downloadService
	Config     *LocalConfig
}

var errNoLocalRecordsForOID = errors.New("no records found for OID")

type metadataStore interface {
	GetObject(ctx context.Context, id string) (*drs.DRSObject, error)
	ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error)
	ListObjectsByProject(ctx context.Context, projectId string) (chan drs.DRSObjectResult, error)
	GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error)
	BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error)
	DeleteRecordsByProject(ctx context.Context, projectId string) error
	DeleteRecord(ctx context.Context, did string) error
	GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error)
	RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error)
	RegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error)
	UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error)
}

type uploadService interface {
	Upload(ctx context.Context, req common.FileUploadRequestObject) error
	ResolveUploadURLs(ctx context.Context, requests []common.UploadURLResolveRequest) ([]common.UploadURLResolveResponse, error)
}

type downloadService interface {
	ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error)
	DownloadToPath(ctx context.Context, guid string, dstPath string, opts download.DownloadOptions) error
}

func NewLocalClient(remote LocalRemote, logger *slog.Logger) *LocalClient {
	dataLogger := logs.NewGen3Logger(logger, "", "")
	req := newLocalRequestInterface(dataLogger)
	if remote.BasicUsername != "" && remote.BasicPassword != "" {
		req = newBasicAuthRequestInterface(req, remote.BasicUsername, remote.BasicPassword)
	}
	dc := drs.NewLocalDrsClient(req, remote.BaseURL, logger)
	tb := transfer.New(req, dataLogger, localsigner.New(remote.BaseURL, req, dc))
	server := drs.ComposeServerClient(dc, tb)

	multiPartThresholdInt := gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)
	multiPartThreshold := int64(multiPartThresholdInt) * common.MB
	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	md := server.
		WithProject(remote.GetProjectId()).
		WithOrganization(remote.GetOrganization()).
		WithBucket(remote.GetBucketName())

	uploader := &serverUploadService{backend: server}
	downloader := &serverDownloadService{
		client:  md,
		backend: server,
		logger:  logger,
	}

	return &LocalClient{
		Remote:     remote,
		Logger:     logger,
		DataLogger: dataLogger,
		meta:       md,
		uploads:    uploader,
		downloads:  downloader,
		Config:     &LocalConfig{MultiPartThreshold: multiPartThreshold, UploadConcurrency: uploadConcurrency},
	}
}

type localRequestInterface struct {
	retryClient *retryablehttp.Client
}

func newLocalRequestInterface(logger *logs.Gen3Logger) request.RequestInterface {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 2
	retryClient.RetryWaitMin = 500 * time.Millisecond
	retryClient.RetryWaitMax = 2 * time.Second
	retryClient.Logger = logger
	retryClient.HTTPClient = &http.Client{Timeout: 0}
	return &localRequestInterface{retryClient: retryClient}
}

func (r *localRequestInterface) New(method, url string) *request.RequestBuilder {
	return &request.RequestBuilder{Method: method, Url: url, Headers: make(map[string]string)}
}

func (r *localRequestInterface) Do(ctx context.Context, rb *request.RequestBuilder) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, rb.Method, rb.Url, rb.Body)
	if err != nil {
		return nil, err
	}
	for key, value := range rb.Headers {
		httpReq.Header.Add(key, value)
	}
	if rb.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+rb.Token)
	}
	if rb.PartSize != 0 {
		httpReq.ContentLength = rb.PartSize
	}
	retryReq, err := retryablehttp.FromRequest(httpReq)
	if err != nil {
		return nil, err
	}
	resp, err := r.retryClient.Do(retryReq)
	if err != nil {
		return resp, fmt.Errorf("request failed after retries: %w", err)
	}
	return resp, nil
}

type basicAuthRequestInterface struct {
	base       request.RequestInterface
	authHeader string
}

func newBasicAuthRequestInterface(base request.RequestInterface, username, password string) request.RequestInterface {
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return &basicAuthRequestInterface{
		base:       base,
		authHeader: "Basic " + encoded,
	}
}

func (b *basicAuthRequestInterface) New(method, url string) *request.RequestBuilder {
	return b.base.New(method, url)
}

func (b *basicAuthRequestInterface) Do(ctx context.Context, rb *request.RequestBuilder) (*http.Response, error) {
	if rb.Headers == nil {
		rb.Headers = map[string]string{}
	}
	if _, exists := rb.Headers["Authorization"]; !exists {
		rb.Headers["Authorization"] = b.authHeader
	}
	return b.base.Do(ctx, rb)
}

func (c *LocalClient) GetProjectId() string                     { return c.Remote.GetProjectId() }
func (c *LocalClient) GetBucketName() string                    { return c.Remote.GetBucketName() }
func (c *LocalClient) GetOrganization() string                  { return c.Remote.Organization }
func (c *LocalClient) GetGen3Interface() g3client.Gen3Interface { return nil }

func (c *LocalClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return c.meta.GetObject(ctx, id)
}
func (c *LocalClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return c.meta.ListObjects(ctx)
}
func (c *LocalClient) ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error) {
	return c.meta.ListObjectsByProject(ctx, project)
}
func (c *LocalClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	u, err := c.downloads.ResolveDownloadURL(ctx, oid, "")
	if err != nil {
		return nil, err
	}
	return &drs.AccessURL{Url: u}, nil
}
func (c *LocalClient) GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error) {
	return c.meta.GetObjectByHash(ctx, checksum)
}
func (c *LocalClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return c.meta.BatchGetObjectsByHash(ctx, hashes)
}
func (c *LocalClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	return c.meta.DeleteRecordsByProject(ctx, project)
}
func (c *LocalClient) DeleteRecordByOID(ctx context.Context, oid string) error {
	records, err := c.meta.GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		return fmt.Errorf("error resolving DRS object for OID %s: %w", oid, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("%w %s", errNoLocalRecordsForOID, oid)
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		did := strings.TrimSpace(record.Id)
		if did == "" {
			continue
		}
		if _, exists := seen[did]; exists {
			continue
		}
		seen[did] = struct{}{}
		if err := c.DeleteRecordByDID(ctx, did); err != nil {
			return fmt.Errorf("error deleting DID %s for OID %s: %w", did, oid, err)
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("no deleteable DIDs found for OID %s", oid)
	}
	return nil
}
func (c *LocalClient) DeleteRecordByDID(ctx context.Context, did string) error {
	return c.meta.DeleteRecord(ctx, did)
}
func (c *LocalClient) GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error) {
	return c.meta.GetProjectSample(ctx, projectId, limit)
}
func (c *LocalClient) RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error) {
	return c.meta.RegisterRecord(ctx, record)
}
func (c *LocalClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return c.meta.RegisterRecords(ctx, records)
}
func (c *LocalClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return c.meta.UpdateRecord(ctx, updateInfo, did)
}

func (c *LocalClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	builder := drs.NewObjectBuilder(c.Remote.GetBucketName(), c.Remote.GetProjectId())
	builder.Organization = c.Remote.Organization
	builder.StoragePrefix = c.Remote.StoragePrefix
	builder.PathStyle = "CAS"
	return builder.Build(fileName, checksum, size, drsId)
}

func (c *LocalClient) RegisterFile(ctx context.Context, oid string, filePath string) (*drs.DRSObject, error) {
	oid = drs.NormalizeOid(oid)
	obj, err := drsmap.DrsInfoFromOid(oid)
	if err != nil || obj == nil {
		stat, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record: %v", statErr)
		}
		drsID := drsmap.DrsUUID(c.Remote.GetProjectId(), oid)
		obj, err = c.BuildDrsObj(filepath.Base(filePath), oid, stat.Size(), drsID)
		if err != nil {
			return nil, err
		}
	}

	registered, err := c.RegisterRecord(ctx, obj)
	if err != nil {
		return nil, err
	}
	obj = registered

	if c.Remote.GetBucketName() == "" {
		return obj, nil
	}

	uploadReq := common.FileUploadRequestObject{
		SourcePath:   filePath,
		ObjectKey:    filepath.Base(filePath),
		GUID:         obj.Id,
		FileMetadata: common.FileMetadata{},
		Bucket:       c.Remote.GetBucketName(),
	}
	if err := c.uploads.Upload(ctx, uploadReq); err != nil {
		return nil, err
	}
	return obj, nil
}

func (c *LocalClient) DownloadFile(ctx context.Context, guid string, destPath string) error {
	opts := download.DownloadOptions{MultipartThreshold: int64(5 * common.GB)}
	if c.Config != nil && c.Config.MultiPartThreshold > 0 {
		opts.MultipartThreshold = c.Config.MultiPartThreshold
	}
	return c.downloads.DownloadToPath(ctx, guid, destPath, opts)
}

func (c *LocalClient) BatchSyncForPush(ctx context.Context, files map[string]lfs.LfsFileInfo) error {
	type pendingUpload struct {
		oid  string
		file lfs.LfsFileInfo
		obj  *drs.DRSObject
	}

	if len(files) == 0 {
		return nil
	}

	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pending := make([]pendingUpload, 0, len(keys))
	toRegister := make([]*drs.DRSObject, 0, len(keys))
	seenOID := make(map[string]struct{}, len(keys))

	for _, k := range keys {
		f := files[k]
		oid := drs.NormalizeOid(f.Oid)
		if oid == "" {
			continue
		}
		if _, dup := seenOID[oid]; dup {
			continue
		}
		seenOID[oid] = struct{}{}

		obj, err := drsmap.DrsInfoFromOid(oid)
		if err != nil || obj == nil {
			stat, statErr := os.Stat(f.Name)
			if statErr != nil {
				return fmt.Errorf("upload failed for %s (%s): error reading local record: %v", f.Name, oid, statErr)
			}
			drsID := drsmap.DrsUUID(c.Remote.GetProjectId(), oid)
			obj, err = c.BuildDrsObj(filepath.Base(f.Name), oid, stat.Size(), drsID)
			if err != nil {
				return fmt.Errorf("upload failed for %s (%s): %w", f.Name, oid, err)
			}
		}

		pending = append(pending, pendingUpload{oid: oid, file: f, obj: obj})
		toRegister = append(toRegister, obj)
	}

	if len(toRegister) == 0 {
		return nil
	}

	registered, err := c.BatchRegisterRecords(ctx, toRegister)
	if err != nil {
		return fmt.Errorf("batch register failed: %w", err)
	}

	registeredByOID := make(map[string]*drs.DRSObject, len(registered))
	for _, obj := range registered {
		if obj == nil {
			continue
		}
		oid := drs.NormalizeOid(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
		if oid != "" {
			registeredByOID[oid] = obj
		}
	}

	if c.Remote.GetBucketName() == "" {
		return nil
	}

	type uploadPlan struct {
		oid string
		req common.FileUploadRequestObject
	}
	uploadPlans := make([]uploadPlan, 0, len(pending))
	for _, p := range pending {
		obj := p.obj
		if reg, ok := registeredByOID[p.oid]; ok && reg != nil {
			obj = reg
		}
		if obj == nil || strings.TrimSpace(obj.Id) == "" {
			return fmt.Errorf("upload failed for %s (%s): missing DRS ID after batch register", p.file.Name, p.oid)
		}
		uploadReq := common.FileUploadRequestObject{
			SourcePath:   p.file.Name,
			ObjectKey:    filepath.Base(p.file.Name),
			GUID:         obj.Id,
			FileMetadata: common.FileMetadata{},
			Bucket:       c.Remote.GetBucketName(),
		}
		uploadPlans = append(uploadPlans, uploadPlan{oid: p.oid, req: uploadReq})
	}

	resolvedByKey := make(map[string]common.UploadURLResolveResponse, len(uploadPlans))
	batchReqs := make([]common.UploadURLResolveRequest, 0, len(uploadPlans))
	for _, plan := range uploadPlans {
		req := plan.req
		batchReqs = append(batchReqs, common.UploadURLResolveRequest{
			GUID:     req.GUID,
			Filename: req.ObjectKey,
			Metadata: req.FileMetadata,
			Bucket:   req.Bucket,
		})
	}
	if len(batchReqs) > 0 {
		resolved, resolveErr := c.uploads.ResolveUploadURLs(ctx, batchReqs)
		if resolveErr != nil {
			c.Logger.WarnContext(ctx, "batch upload URL resolve failed; falling back to per-file resolve", "error", resolveErr)
		} else {
			for _, res := range resolved {
				resolvedByKey[res.GUID+"|"+res.Filename] = res
			}
		}
	}

	for _, plan := range uploadPlans {
		uploadReq := plan.req
		if res, ok := resolvedByKey[uploadReq.GUID+"|"+uploadReq.ObjectKey]; ok {
			if res.Status >= 200 && res.Status < 300 && strings.TrimSpace(res.URL) != "" {
				uploadReq.PresignedURL = res.URL
			} else if res.Error != "" {
				c.Logger.WarnContext(ctx, "batch upload URL resolve returned non-success; falling back to per-file resolve",
					"guid", uploadReq.GUID,
					"file", uploadReq.ObjectKey,
					"status", res.Status,
					"error", res.Error,
				)
			}
		}
		if err := c.uploads.Upload(ctx, uploadReq); err != nil {
			return fmt.Errorf("upload failed for %s (%s): %w", uploadReq.SourcePath, plan.oid, err)
		}
	}

	return nil
}

type serverUploadService struct {
	backend transfer.Uploader
}

func (s *serverUploadService) Upload(ctx context.Context, req common.FileUploadRequestObject) error {
	return upload.Upload(ctx, s.backend, req, false)
}

func (s *serverUploadService) ResolveUploadURLs(ctx context.Context, requests []common.UploadURLResolveRequest) ([]common.UploadURLResolveResponse, error) {
	return s.backend.ResolveUploadURLs(ctx, requests)
}

type serverDownloadService struct {
	client  drs.Client
	backend transfer.Downloader
	logger  *slog.Logger
}

type normalizedDownloadBackend struct {
	base     transfer.Downloader
	resolver *serverDownloadService
}

func (n *normalizedDownloadBackend) Name() string {
	return n.base.Name()
}

func (n *normalizedDownloadBackend) Logger() *logs.Gen3Logger {
	return n.base.Logger()
}

func (n *normalizedDownloadBackend) ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	return n.resolver.ResolveDownloadURL(ctx, guid, accessID)
}

func (n *normalizedDownloadBackend) Download(ctx context.Context, fdr *common.FileDownloadResponseObject) (*http.Response, error) {
	return n.base.Download(ctx, fdr)
}

func (s *serverDownloadService) ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	obj, err := drs.ResolveObject(ctx, s.client, guid)
	if err != nil {
		return "", err
	}

	resolvedID := strings.TrimSpace(obj.Id)
	if resolvedID == "" {
		resolvedID = guid
	}

	candidates := make([]string, 0, len(obj.AccessMethods)+3)
	seen := make(map[string]struct{}, len(obj.AccessMethods)+3)
	addCandidate := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		candidates = append(candidates, v)
	}

	addCandidate(accessID)
	for _, am := range obj.AccessMethods {
		if am.AccessId != nil {
			addCandidate(*am.AccessId)
		}
		addCandidate(am.Type)
	}
	addCandidate("s3")

	var lastErr error
	for _, candidate := range candidates {
		accessURL, getErr := s.client.GetDownloadURL(ctx, resolvedID, candidate)
		if getErr != nil {
			lastErr = getErr
			continue
		}
		if accessURL == nil || strings.TrimSpace(accessURL.Url) == "" {
			continue
		}
		if !isHTTPURL(accessURL.Url) {
			continue
		}
		return accessURL.Url, nil
	}

	// Fallback to direct access URLs only when they are already HTTP(S).
	for _, am := range obj.AccessMethods {
		if am.AccessUrl == nil || strings.TrimSpace(am.AccessUrl.Url) == "" {
			continue
		}
		if isHTTPURL(am.AccessUrl.Url) {
			return am.AccessUrl.Url, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("failed to resolve signed download URL for %s: %w", guid, lastErr)
	}
	return "", fmt.Errorf("no usable HTTP(S) download URL available for %s", guid)
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

func (s *serverDownloadService) DownloadToPath(ctx context.Context, guid string, dstPath string, opts download.DownloadOptions) error {
	backend := &normalizedDownloadBackend{
		base:     s.backend,
		resolver: s,
	}
	return download.DownloadToPathWithOptions(ctx, s.client, backend, s.logger, guid, dstPath, "", opts)
}
