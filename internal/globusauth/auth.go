package globusauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/authorizers"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/core"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/login"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/services/transfer"
	"github.com/scttfrdmn/globus-go-sdk/v4/pkg/tokenstorage"
)

const (
	TransferScope       = "urn:globus:auth:scope:transfer.api.globus.org:all"
	TransferTokenEnv    = "GIT_DRS_GLOBUS_TRANSFER_TOKEN"
	ClientIDEnv         = "GIT_DRS_GLOBUS_CLIENT_ID"
	ClientSecretEnv     = "GIT_DRS_GLOBUS_CLIENT_SECRET"
	TokenFileEnv        = "GIT_DRS_GLOBUS_TOKEN_FILE"
	transferResource    = "transfer.api.globus.org"
	defaultTokenFile    = "globus-tokens.json"
	defaultClientIDFile = "globus-client.json"
)

var ErrMissingToken = fmt.Errorf("Globus authentication is required; run `git drs auth globus login` or set %s", TransferTokenEnv)

type Readiness string

const (
	Ready    Readiness = "ready"
	Disabled Readiness = "disabled"
	Broken   Readiness = "broken"
)

// CredentialReadiness checks local configuration without contacting Globus.
func CredentialReadiness() (Readiness, string) {
	if strings.TrimSpace(os.Getenv(TransferTokenEnv)) != "" {
		return Ready, "environment token configured"
	}
	name, err := tokenFile()
	if err != nil {
		return Broken, err.Error()
	}
	if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
		return Disabled, ErrMissingToken.Error()
	} else if err != nil {
		return Broken, err.Error()
	}
	storage, err := tokenstorage.NewJSONTokenStorageWithNamespace(name, "git-drs")
	if err != nil {
		return Broken, err.Error()
	}
	defer storage.Close()
	token, err := storage.Get(transferResource)
	if err != nil {
		return Broken, err.Error()
	}
	if token == nil {
		return Disabled, ErrMissingToken.Error()
	}
	if token.RefreshToken == "" && token.IsExpired() {
		return Broken, "stored Globus access token is expired and cannot be refreshed"
	}
	if token.RefreshToken != "" {
		if _, err := configuredClientID(name); err != nil {
			return Broken, err.Error()
		}
	}
	return Ready, "stored Globus credential configured"
}

type Client struct {
	transfer   *transfer.Client
	storage    tokenstorage.TokenStorage
	authorizer core.Authorizer
	httpClient *http.Client
	baseURL    string
}

type TransferItem struct {
	SourcePath, DestinationPath, SHA256 string
}

type File struct {
	Path string
	Size int64
}

func tokenFile() (string, error) {
	if name := strings.TrimSpace(os.Getenv(TokenFileEnv)); name != "" {
		return name, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "git-drs", defaultTokenFile), nil
}

func openStorage() (tokenstorage.TokenStorage, string, error) {
	name, err := tokenFile()
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return nil, "", fmt.Errorf("create Globus credential directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(name), 0o700); err != nil {
		return nil, "", fmt.Errorf("secure Globus credential directory: %w", err)
	}
	storage, err := tokenstorage.NewJSONTokenStorageWithNamespace(name, "git-drs")
	if err != nil {
		return nil, "", fmt.Errorf("open Globus token storage: %w", err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		storage.Close()
		return nil, "", fmt.Errorf("secure Globus token storage: %w", err)
	}
	return storage, name, nil
}

func clientIDFile(tokenName string) string {
	return filepath.Join(filepath.Dir(tokenName), defaultClientIDFile)
}

func configuredClientID(tokenName string) (string, error) {
	if id := strings.TrimSpace(os.Getenv(ClientIDEnv)); id != "" {
		return id, nil
	}
	data, err := os.ReadFile(clientIDFile(tokenName))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("Globus native application client ID is required; set %s", ClientIDEnv)
	}
	if err != nil {
		return "", fmt.Errorf("read Globus client configuration: %w", err)
	}
	var config struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse Globus client configuration: %w", err)
	}
	if strings.TrimSpace(config.ClientID) == "" {
		return "", fmt.Errorf("Globus client configuration has no client ID; set %s", ClientIDEnv)
	}
	return strings.TrimSpace(config.ClientID), nil
}

func saveClientID(tokenName, clientID string) error {
	data, err := json.Marshal(struct {
		ClientID string `json:"client_id"`
	}{ClientID: clientID})
	if err != nil {
		return err
	}
	if err := os.WriteFile(clientIDFile(tokenName), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("save Globus client configuration: %w", err)
	}
	return nil
}

// Login runs the SDK's PKCE command-line OAuth flow and stores refreshable
// tokens. Additional scopes may include collection data_access scopes.
func Login(ctx context.Context, additionalScopes []string) (string, error) {
	storage, tokenName, err := openStorage()
	if err != nil {
		return "", err
	}
	defer storage.Close()
	clientID, err := configuredClientID(tokenName)
	if err != nil {
		return "", err
	}
	manager := login.NewCommandLineLoginFlowManager(clientID, strings.TrimSpace(os.Getenv(ClientSecretEnv)))
	scopes := append([]string{TransferScope}, additionalScopes...)
	result, err := manager.RunLoginFlow(ctx, login.AuthParams{Scopes: scopes, RequestRefresh: true})
	if err != nil {
		return "", err
	}
	for _, token := range result.Tokens {
		if err := storage.Store(token); err != nil {
			return "", fmt.Errorf("store Globus token for %s: %w", token.ResourceServer, err)
		}
	}
	if err := saveClientID(tokenName, clientID); err != nil {
		return "", err
	}
	return tokenName, nil
}

func Logout() error {
	storage, _, err := openStorage()
	if err != nil {
		return err
	}
	defer storage.Close()
	tokens, err := storage.GetAll()
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if err := storage.Remove(token.ResourceServer); err != nil {
			return err
		}
	}
	return nil
}

func NewClient(ctx context.Context) (*Client, error) {
	return newClient(ctx, true)
}

func newClient(ctx context.Context, allowEnvironmentToken bool) (*Client, error) {
	var (
		authorizer core.Authorizer
		storage    tokenstorage.TokenStorage
	)
	if token := strings.TrimSpace(os.Getenv(TransferTokenEnv)); allowEnvironmentToken && token != "" {
		authorizer = authorizers.NewAccessTokenAuthorizer(token)
	} else {
		var tokenName string
		var err error
		storage, tokenName, err = openStorage()
		if err != nil {
			return nil, err
		}
		token, err := storage.Get(transferResource)
		if err != nil {
			storage.Close()
			return nil, fmt.Errorf("load Globus Transfer token: %w", err)
		}
		if token == nil {
			storage.Close()
			return nil, ErrMissingToken
		}
		if token.RefreshToken == "" {
			if token.IsExpired() {
				storage.Close()
				return nil, ErrMissingToken
			}
			authorizer = authorizers.NewAccessTokenAuthorizer(token.AccessToken)
		} else {
			clientID, err := configuredClientID(tokenName)
			if err != nil {
				storage.Close()
				return nil, err
			}
			authorizer = authorizers.NewRefreshTokenAuthorizer(
				token.RefreshToken,
				clientID,
				strings.TrimSpace(os.Getenv(ClientSecretEnv)),
				authorizers.WithInitialAccessToken(token.AccessToken, token.ExpiresAt),
				authorizers.WithOnRefresh(func(accessToken, refreshToken string, expiresAt time.Time) {
					token.AccessToken = accessToken
					token.RefreshToken = refreshToken
					token.ExpiresAt = expiresAt
					_ = storage.Store(token)
				}),
			)
		}
	}

	config := &core.Config{Authorizer: authorizer, Scopes: []string{TransferScope}}
	sdkClient, err := transfer.NewClient(ctx, config)
	if err != nil {
		if storage != nil {
			storage.Close()
		}
		return nil, err
	}
	return &Client{transfer: sdkClient, storage: storage, authorizer: authorizer, httpClient: http.DefaultClient, baseURL: "https://transfer.api.globus.org"}, nil
}

func CheckStored(ctx context.Context) error {
	client, err := newClient(ctx, false)
	if err != nil {
		return err
	}
	defer client.Close()
	return Check(ctx, client)
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.storage != nil {
		defer c.storage.Close()
	}
	if c.transfer != nil {
		return c.transfer.Close()
	}
	return nil
}

func Check(ctx context.Context, client *Client) error {
	owned := client == nil
	if owned {
		var err error
		client, err = NewClient(ctx)
		if err != nil {
			return err
		}
		defer client.Close()
	}
	if client == nil || client.transfer == nil {
		return fmt.Errorf("nil Globus client")
	}
	if _, err := client.transfer.ListTasks(ctx, &transfer.ListTasksOptions{Limit: 1}); err != nil {
		return fmt.Errorf("Globus Transfer API authentication failed: %w", err)
	}
	return nil
}

func (c *Client) SubmitTransfer(ctx context.Context, srcCollection, srcPath, dstCollection, dstPath, label string) (string, error) {
	return c.SubmitTransferItems(ctx, srcCollection, dstCollection, []TransferItem{{SourcePath: srcPath, DestinationPath: dstPath}}, label)
}

func (c *Client) SubmitTransferItems(ctx context.Context, srcCollection, dstCollection string, items []TransferItem, label string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("submit Globus transfer: no items")
	}
	sdkItems := make([]transfer.TransferItem, len(items))
	for i, item := range items {
		sdkItems[i] = transfer.TransferItem{DATA_TYPE: "transfer_item", SourcePath: item.SourcePath, DestinationPath: item.DestinationPath}
		if item.SHA256 != "" {
			sdkItems[i].ExternalChecksum = item.SHA256
			sdkItems[i].ChecksumAlgorithm = "SHA256"
		}
	}
	response, err := c.transfer.SubmitTransfer(ctx, &transfer.Transfer{
		SourceEndpoint:      srcCollection,
		DestinationEndpoint: dstCollection,
		Label:               label,
		SyncLevel:           3,
		VerifyChecksum:      true,
		Items:               sdkItems,
	})
	if err != nil {
		return "", fmt.Errorf("submit Globus transfer: %w", err)
	}
	if strings.TrimSpace(response.TaskID) == "" {
		return "", fmt.Errorf("submit Globus transfer returned an empty task id")
	}
	return strings.TrimSpace(response.TaskID), nil
}

func (c *Client) ListFiles(ctx context.Context, collection, root string) ([]File, error) {
	root = strings.TrimRight(root, "/") + "/"
	queue := []string{root}
	var files []File
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		listing, err := c.listDirectory(ctx, collection, dir)
		if err != nil {
			return nil, fmt.Errorf("list Globus directory %s:%s: %w", collection, dir, err)
		}
		for _, entry := range listing {
			itemPath := strings.TrimRight(dir, "/") + "/" + entry.Name
			switch entry.Type {
			case "file":
				files = append(files, File{Path: itemPath, Size: entry.Size})
			case "dir":
				queue = append(queue, itemPath+"/")
			case "symlink":
				return nil, fmt.Errorf("Globus collection import does not support symlink %s", itemPath)
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

type directoryEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

func (c *Client) listDirectory(ctx context.Context, collection, dir string) ([]directoryEntry, error) {
	query := url.Values{"path": {dir}, "show_hidden": {"1"}}
	rawURL := strings.TrimRight(c.baseURL, "/") + "/v0.10/operation/endpoint/" + url.PathEscape(collection) + "/ls?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if c.authorizer != nil {
		header, err := c.authorizer.GetAuthorizationHeader(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", header)
	}
	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Globus directory listing returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var listing struct {
		Data []directoryEntry `json:"DATA"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		return nil, fmt.Errorf("decode Globus directory listing: %w", err)
	}
	return listing.Data, nil
}

func (c *Client) WaitForTask(ctx context.Context, taskID string, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	for {
		task, err := c.transfer.GetTask(ctx, taskID)
		if err != nil {
			return fmt.Errorf("get Globus transfer task %s: %w", taskID, err)
		}
		status := strings.ToUpper(strings.TrimSpace(task.Status))
		switch status {
		case "SUCCEEDED":
			return nil
		case "FAILED":
			fallthrough
		case "INACTIVE":
			if status == "INACTIVE" && task.FatalError == nil {
				break
			}
			detail := strings.TrimSpace(task.NiceStatus)
			if task.FatalError != nil && strings.TrimSpace(task.FatalError.Description) != "" {
				detail = strings.TrimSpace(task.FatalError.Description)
			}
			if detail != "" {
				return fmt.Errorf("Globus transfer task %s failed: %s", taskID, detail)
			}
			return fmt.Errorf("Globus transfer task %s failed", taskID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}
