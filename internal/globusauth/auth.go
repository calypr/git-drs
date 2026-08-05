package globusauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	TransferAPIBaseURL = "https://transfer.api.globus.org/v0.10"
	TransferScope      = "urn:globus:auth:scope:transfer.api.globus.org:all"
	TransferTokenEnv   = "GIT_DRS_GLOBUS_TRANSFER_TOKEN"
)

var ErrMissingToken = fmt.Errorf("Globus Transfer API token is required; set %s to a Globus Auth access token scoped for %s", TransferTokenEnv, TransferScope)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

func NewClientFromEnv() (*Client, error) {
	token := strings.TrimSpace(os.Getenv(TransferTokenEnv))
	if token == "" {
		return nil, ErrMissingToken
	}
	return &Client{BaseURL: TransferAPIBaseURL, HTTPClient: http.DefaultClient, Token: token}, nil
}

func Check(ctx context.Context, client *Client) (string, error) {
	if client == nil {
		var err error
		client, err = NewClientFromEnv()
		if err != nil {
			return "", err
		}
	}
	var summary struct {
		Username string `json:"username"`
	}
	if err := client.Do(ctx, http.MethodGet, "/tasksummary", nil, &summary); err != nil {
		return "", fmt.Errorf("Globus Transfer API authentication failed: %w", err)
	}
	return strings.TrimSpace(summary.Username), nil
}

func (c *Client) SubmitTransfer(ctx context.Context, srcCollection, srcPath, dstCollection, dstPath, label string) (string, error) {
	var sub struct {
		Value string `json:"value"`
	}
	if err := c.Do(ctx, http.MethodGet, "/submission_id", nil, &sub); err != nil {
		return "", fmt.Errorf("get Globus submission id: %w", err)
	}
	payload := map[string]any{
		"DATA_TYPE":            "transfer",
		"submission_id":        sub.Value,
		"source_endpoint":      srcCollection,
		"destination_endpoint": dstCollection,
		"label":                label,
		"sync_level":           "checksum",
		"DATA": []map[string]any{{
			"DATA_TYPE":        "transfer_item",
			"source_path":      srcPath,
			"destination_path": dstPath,
		}},
	}
	var out struct {
		TaskID string `json:"task_id"`
	}
	if err := c.Do(ctx, http.MethodPost, "/transfer", payload, &out); err != nil {
		return "", fmt.Errorf("submit Globus transfer: %w", err)
	}
	if strings.TrimSpace(out.TaskID) == "" {
		return "", fmt.Errorf("submit Globus transfer returned an empty task id")
	}
	return strings.TrimSpace(out.TaskID), nil
}

func (c *Client) WaitForTask(ctx context.Context, taskID string, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	for {
		var task struct {
			Status       string `json:"status"`
			NiceStatus   string `json:"nice_status"`
			Faults       int    `json:"faults"`
			SubtasksDone int    `json:"subtasks_succeeded"`
		}
		if err := c.Do(ctx, http.MethodGet, "/task/"+taskID, nil, &task); err != nil {
			return fmt.Errorf("get Globus transfer task %s: %w", taskID, err)
		}
		switch strings.ToUpper(strings.TrimSpace(task.Status)) {
		case "SUCCEEDED":
			return nil
		case "FAILED":
			if task.NiceStatus != "" {
				return fmt.Errorf("Globus transfer task %s failed: %s", taskID, task.NiceStatus)
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

func (c *Client) Do(ctx context.Context, method, apiPath string, in, out any) error {
	if c == nil {
		return fmt.Errorf("nil Globus client")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = TransferAPIBaseURL
	}
	var body io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/"+strings.TrimLeft(apiPath, "/"), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.Token))
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("%s %s returned %d%s", method, apiPath, resp.StatusCode, ResponseBodySuffix(data))
	}
	if readErr != nil {
		return readErr
	}
	if out != nil && len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return err
		}
	}
	return nil
}

func ResponseBodySuffix(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	return ": " + trimmed
}
