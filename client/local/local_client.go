package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/download"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	s3utils "github.com/calypr/data-client/s3utils"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/cloud"
	gitdrsCommon "github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/drsmap"
)

type LocalRemote struct {
	BaseURL      string
	ProjectID    string
	Bucket       string
	Organization string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}

func (l LocalRemote) GetEndpoint() string {
	return l.BaseURL
}

func (l LocalRemote) GetBucketName() string {
	return l.Bucket
}

func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	return NewLocalClient(l, logger), nil
}

type LocalClient struct {
	Remote LocalRemote
	Logger *slog.Logger
}

func NewLocalClient(remote LocalRemote, logger *slog.Logger) *LocalClient {
	return &LocalClient{
		Remote: remote,
		Logger: logger,
	}
}

// Helpers

func (c *LocalClient) buildURL(paths ...string) (string, error) {
	u, err := url.Parse(c.Remote.BaseURL)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(append([]string{u.Path}, paths...)...)
	return u.String(), nil
}

func (c *LocalClient) doJSONRequest(ctx context.Context, method, url string, body interface{}, dst interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request to %s failed with status %d: %s", url, resp.StatusCode, string(bodyBytes))
	}

	if dst != nil {
		return json.NewDecoder(resp.Body).Decode(dst)
	}
	return nil
}

// Implement DRSClient interface

func (c *LocalClient) GetProjectId() string {
	return c.Remote.GetProjectId()
}

func (c *LocalClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	// Standard DRS: GET /ga4gh/drs/v1/objects/{id}
	u, err := c.buildURL("ga4gh/drs/v1/objects", id)
	if err != nil {
		return nil, err
	}

	var obj drs.DRSObject
	if err := c.doJSONRequest(ctx, http.MethodGet, u, nil, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
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

	// 3. Call /access endpoint
	u, err := c.buildURL("ga4gh/drs/v1/objects", oid, "access", accessID)
	if err != nil {
		return nil, err
	}
	// Note: protocol/accessType might be query param? Standard says /access/{access_id}
	// Some implementations use query params. Sticking to path.

	var accessURL drs.AccessURL
	if err := c.doJSONRequest(ctx, http.MethodGet, u, nil, &accessURL); err != nil {
		return nil, err
	}
	return &accessURL, nil
}

func (c *LocalClient) GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error) {
	// Query: GET /ga4gh/drs/v1/objects/checksum/<hash>
	u, err := c.buildURL("ga4gh/drs/v1/objects", "checksum", checksum.Checksum)
	if err != nil {
		return nil, err
	}

	var objs []drs.DRSObject
	if err := c.doJSONRequest(ctx, http.MethodGet, u, nil, &objs); err != nil {
		return nil, err
	}

	return objs, nil
}

func (c *LocalClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	return nil
}

func (c *LocalClient) DeleteRecord(ctx context.Context, oid string) error {
	return nil
}

// RegisterRecord registers a DRS object with the server
func (c *LocalClient) RegisterRecord(ctx context.Context, indexdObject *drs.DRSObject) (*drs.DRSObject, error) {
	u, err := c.buildURL("ga4gh/drs/v1/objects/register")
	if err != nil {
		return nil, err
	}

	req := drs.RegisterObjectsRequest{
		Candidates: []drs.DRSObjectCandidate{drs.ConvertToCandidate(indexdObject)},
	}

	// Debug: log the request
	jsonBytes, _ := json.Marshal(req)
	c.Logger.Debug("RegisterRecord request", "url", u, "json", string(jsonBytes))

	var registeredObjs []drs.DRSObject
	if err := c.doJSONRequest(ctx, http.MethodPost, u, req, &registeredObjs); err != nil {
		return nil, err
	}

	if len(registeredObjs) == 0 {
		return nil, fmt.Errorf("server returned no registered objects")
	}

	return &registeredObjs[0], nil
}

func (c *LocalClient) RegisterFile(ctx context.Context, oid string, filePath string) (*drs.DRSObject, error) {
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		// Fallback: if record is missing locally, try to build it from the file on disk
		stat, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record for oid %s: %v (also failed to stat file %s: %v)", oid, err, filePath, statErr)
		}

		drsId := drsmap.DrsUUID(c.Remote.GetProjectId(), oid)
		drsObject, err = c.BuildDrsObj(filepath.Base(filePath), oid, stat.Size(), drsId)
		if err != nil {
			return nil, fmt.Errorf("error building drs info for oid %s: %v", oid, err)
		}
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for %s: %v", filePath, err)
	}

	resPath, err := gitdrsCommon.ProjectToResource(c.Remote.Organization, c.Remote.GetProjectId())
	if err != nil {
		return nil, fmt.Errorf("failed to convert project ID to resource: %v", err)
	}

	targetURL := "file://" + absPath
	found := false
	for i := range drsObject.AccessMethods {
		if drsObject.AccessMethods[i].AccessURL.URL == targetURL {
			found = true
			if drsObject.AccessMethods[i].Authorizations == nil {
				drsObject.AccessMethods[i].Authorizations = &drs.Authorizations{}
			}
			// Add unique project ID to BearerAuthIssuers
			exists := false
			for _, issuer := range drsObject.AccessMethods[i].Authorizations.BearerAuthIssuers {
				if issuer == resPath {
					exists = true
					break
				}
			}
			if !exists {
				drsObject.AccessMethods[i].Authorizations.BearerAuthIssuers = append(
					drsObject.AccessMethods[i].Authorizations.BearerAuthIssuers,
					resPath,
				)
			}
			// Sync legacy Value field
			drsObject.AccessMethods[i].Authorizations.Value = resPath
			break
		}
	}

	if !found {
		drsObject.AccessMethods = append(drsObject.AccessMethods, drs.AccessMethod{
			Type: "file",
			AccessURL: drs.AccessURL{
				URL: targetURL,
			},
			Region: "",
			Authorizations: &drs.Authorizations{
				Value:             resPath,
				BearerAuthIssuers: []string{resPath},
			},
		})
	}

	return c.RegisterRecord(ctx, drsObject)
}

func (c *LocalClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return nil, fmt.Errorf("UpdateRecord not implemented for LocalClient")
}

func (c *LocalClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	return drs.BuildDrsObj(fileName, checksum, size, drsId, c.Remote.GetBucketName(), c.Remote.Organization, c.Remote.GetProjectId())
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

// DownloadFile implementation

type RealLocalDownloadStrategy struct {
	Client *LocalClient
}

func (s *RealLocalDownloadStrategy) GetDownloadResponse(ctx context.Context, fdr *common.FileDownloadResponseObject, protocolText string) error {
	// Use client to resolve URL
	accessURL, err := s.Client.GetDownloadURL(ctx, fdr.GUID)
	if err != nil {
		return err
	}
	fdr.PresignedURL = accessURL.URL

	// Handle file:// scheme
	if strings.HasPrefix(fdr.PresignedURL, "file://") {
		filePath := strings.TrimPrefix(fdr.PresignedURL, "file://")
		f, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open local file %s: %w", filePath, err)
		}
		stat, err := f.Stat()
		if err != nil {
			f.Close()
			return fmt.Errorf("failed to stat local file %s: %w", filePath, err)
		}

		// Create a synthetic response
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			Body:          f,
			ContentLength: stat.Size(),
		}
		fdr.Response = resp
		return nil
	}

	// Perform actual download request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fdr.PresignedURL, nil)
	if err != nil {
		return err
	}

	// Add headers from AccessURL if needed
	if len(accessURL.Headers) > 0 {
		for _, header := range accessURL.Headers {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) == 2 {
				req.Header.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
			}
		}
	}

	if fdr.Range > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", fdr.Range))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	fdr.Response = resp
	return nil
}

func (c *LocalClient) DownloadFile(ctx context.Context, oid string, destPath string) error {
	strategy := &RealLocalDownloadStrategy{Client: c}
	return download.DownloadToPath(ctx, strategy, c.Logger, oid, destPath, "")
}
