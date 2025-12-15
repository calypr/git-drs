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
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"golang.org/x/oauth2/google"
)

type AnvilClient struct {
	Endpoint string
	sConfig  sonic.API
}

func (an *AnvilClient) GetObject(objectID string) (*drs.DRSObject, error) {
	// get auth token
	token, err := GetAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth token: %w", err)
	}

	reqBody := map[string]any{
		"url":    objectID,
		"fields": []string{"hashes", "size", "fileName"},
	}
	bodyBytes, err := an.sConfig.Marshal(reqBody)
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
		an.sConfig.Unmarshal(respBody, &errResp)
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
	if err := an.sConfig.Unmarshal(respBody, &parsed); err != nil {
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

// GetAuthToken fetches a Google Cloud authentication token using Application Default Credentials.
// The user must run `gcloud auth application-default login` before using this.
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
