package anvil_client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bytedance/sonic"
	drs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	hash "github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/s3utils"
	"github.com/calypr/git-drs/cloud"
	"golang.org/x/oauth2/google"
)

type AnvilClient struct {
	Endpoint string
	SConfig  sonic.API
}

func (an *AnvilClient) GetObject(ctx context.Context, objectID string) (*drs.DRSObject, error) {
	// get auth token
	token, err := GetAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}

	reqBody := map[string]any{
		"url":    objectID,
		"fields": []string{"hashes", "size", "fileName"},
	}
	bodyBytes, err := an.SConfig.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", an.Endpoint, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode > 399 {
		// Try to extract error message
		var errResp map[string]any
		an.SConfig.Unmarshal(respBody, &errResp)
		msg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
		if m, ok := errResp["message"].(string); ok {
			msg = m
		}
		return &drs.DRSObject{}, errors.New(msg)
	}

	// Parse expected response
	// subset of ResourceMetadata
	// https://github.com/DataBiosphere/terra-drs-hub/blob/dev/common/openapi.yml#L123
	var parsed struct {
		Hashes   map[string]string `json:"hashes"`
		Size     int64             `json:"size"`
		FileName string            `json:"fileName"`
	}
	if err := an.SConfig.Unmarshal(respBody, &parsed); err != nil {
		return nil, err
	}

	return &drs.DRSObject{
		SelfURI:   objectID,
		Id:        objectID,
		Checksums: hash.ConvertStringMapToHashInfo(parsed.Hashes),
		Size:      parsed.Size,
		Name:      parsed.FileName,
	}, nil
}

func (an *AnvilClient) GetProjectId() string {
	return ""
}

func (an *AnvilClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) GetObjectByHash(ctx context.Context, hash *hash.Checksum) ([]drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) DownloadFile(ctx context.Context, oid string, destPath string) error {
	return errors.New("method not implemented")
}

func (an *AnvilClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	return errors.New("method not implemented")
}

func (an *AnvilClient) DeleteRecord(ctx context.Context, oid string) error {
	return errors.New("method not implemented")
}

func (an *AnvilClient) RegisterRecord(ctx context.Context, indexdObject *drs.DRSObject) (*drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) RegisterFile(ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	return nil, errors.New("method not implemented")
}

func (an *AnvilClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...cloud.AddURLOption) (s3utils.S3Meta, error) {
	return s3utils.S3Meta{}, errors.New("method not implemented")
}

func (an *AnvilClient) GetBucketName() string {
	return ""
}

func (an *AnvilClient) GetOrganization() string {
	return ""
}

func (an *AnvilClient) GetGen3Interface() g3client.Gen3Interface {
	return nil
}

// GetAuthToken fetches a Google Cloud authentication token using Application Default Credentials.
func GetAuthToken() (string, error) {
	ctx := context.Background()
	creds, err := google.FindDefaultCredentials(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get default credentials: %w", err)
	}

	ts := creds.TokenSource
	token, err := ts.Token()
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	if !token.Valid() || token.AccessToken == "" {
		return "", fmt.Errorf("no token retrieved")
	}

	return token.AccessToken, nil
}
