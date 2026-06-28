package addurl

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/remoteruntime"
	sycloud "github.com/calypr/syfon/client/cloud"
	syrequest "github.com/calypr/syfon/client/request"
)

const internalInspectObjectPath = "/data/inspect"

type inspectedObject struct {
	objectURL string
	info      *sycloud.ObjectInfo
}

type internalInspectObjectRequest struct {
	Organization string `json:"organization,omitempty"`
	Project      string `json:"project,omitempty"`
	Key          string `json:"key,omitempty"`
	Scheme       string `json:"scheme,omitempty"`
	ObjectURL    string `json:"object_url,omitempty"`
}

type internalInspectObjectResponse struct {
	ObjectURL   string `json:"object_url"`
	Provider    string `json:"provider"`
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	Path        string `json:"path"`
	SizeBytes   int64  `json:"size_bytes"`
	MetaSHA256  string `json:"meta_sha256,omitempty"`
	ETag        string `json:"etag,omitempty"`
	LastModTime string `json:"last_modified,omitempty"`
}

func inspectRemoteObjectViaServer(ctx context.Context, drsCtx *remoteruntime.GitContext, input addURLInput) (*inspectedObject, error) {
	if drsCtx == nil || drsCtx.Client == nil || drsCtx.Client.Requestor() == nil {
		return nil, fmt.Errorf("remote-backed add-url inspection requires a configured syfon client")
	}

	req := internalInspectObjectRequest{}
	target := strings.TrimSpace(input.sourceArg)
	if looksLikeCloudURL(target) {
		if !strings.HasPrefix(strings.ToLower(target), "s3://") {
			return nil, fmt.Errorf("remote-backed add-url inspection currently supports only s3:// URLs")
		}
		req.ObjectURL = target
	} else {
		if strings.TrimSpace(input.scheme) == "" {
			return nil, fmt.Errorf("object key mode requires --scheme because the remote must know which provider to inspect")
		}
		if strings.ToLower(strings.TrimSpace(input.scheme)) != "s3" {
			return nil, fmt.Errorf("remote-backed add-url inspection currently supports only --scheme s3")
		}
		req.Organization = strings.TrimSpace(drsCtx.Organization)
		req.Project = strings.TrimSpace(drsCtx.ProjectId)
		req.Key = strings.Trim(target, "/")
		req.Scheme = "s3"
	}

	var resp internalInspectObjectResponse
	if err := drsCtx.Client.Requestor().Do(ctx, http.MethodPost, internalInspectObjectPath, req, &resp); err != nil {
		return nil, mapInspectError(target, err)
	}
	return &inspectedObject{
		objectURL: resp.ObjectURL,
		info: &sycloud.ObjectInfo{
			Bucket:      resp.Bucket,
			Key:         resp.Key,
			Path:        resp.Path,
			SizeBytes:   resp.SizeBytes,
			MetaSHA256:  resp.MetaSHA256,
			ETag:        resp.ETag,
			LastModTime: parseInspectLastModified(resp.LastModTime),
		},
	}, nil
}

func parseInspectLastModified(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

func mapInspectError(target string, err error) error {
	var respErr *syrequest.ResponseError
	if !errorAsResponse(err, &respErr) {
		return fmt.Errorf("remote-backed add-url inspection failed for %q: %w", target, err)
	}

	body := strings.TrimSpace(respErr.Body)
	switch respErr.Status {
	case http.StatusBadRequest:
		return fmt.Errorf("remote-backed add-url inspection rejected %q: %s", target, fallbackInspectMessage(body, "invalid request"))
	case http.StatusForbidden:
		return fmt.Errorf("remote-backed add-url inspection was denied for %q: %s", target, fallbackInspectMessage(body, "permission denied"))
	case http.StatusNotFound:
		if looksLikeInspectRouteMissing(body) {
			return fmt.Errorf("remote-backed add-url inspection is unavailable on this Syfon remote; upgrade Syfon to a version that implements %s", internalInspectObjectPath)
		}
		return fmt.Errorf("remote-backed add-url inspection could not find %q: %s", target, fallbackInspectMessage(body, "not found"))
	default:
		return fmt.Errorf("remote-backed add-url inspection failed for %q: %s", target, fallbackInspectMessage(body, err.Error()))
	}
}

func looksLikeInspectRouteMissing(body string) bool {
	body = strings.ToLower(strings.TrimSpace(body))
	return body == "" ||
		strings.Contains(body, "cannot post /data/inspect") ||
		strings.Contains(body, "cannot post /data/inspect/") ||
		strings.Contains(body, "not found")
}

func fallbackInspectMessage(body string, fallback string) string {
	if strings.TrimSpace(body) != "" {
		return strings.TrimSpace(body)
	}
	return fallback
}

func errorAsResponse(err error, target **syrequest.ResponseError) bool {
	respErr, ok := err.(*syrequest.ResponseError)
	if ok {
		*target = respErr
		return true
	}
	return false
}
