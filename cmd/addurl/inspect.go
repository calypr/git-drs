package addurl

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/remoteruntime"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	"github.com/calypr/syfon/client/apierror"
	sycloud "github.com/calypr/syfon/client/cloud"
)

const internalInspectObjectPath = "/data/inspect"

type inspectedObject struct {
	objectURL string
	info      *sycloud.ObjectInfo
}

func inspectRemoteObjectViaServer(ctx context.Context, drsCtx *remoteruntime.GitContext, input addURLInput) (*inspectedObject, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("remote-backed add-url inspection requires a configured syfon client")
	}

	req := internalapi.InternalInspectObjectRequest{}
	target := strings.TrimSpace(input.sourceArg)
	if looksLikeCloudURL(target) {
		if !strings.HasPrefix(strings.ToLower(target), "s3://") {
			return nil, fmt.Errorf("remote-backed add-url inspection currently supports only s3:// URLs")
		}
		req.ObjectUrl = target
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

	resp, err := drsCtx.Client.InternalAPI().InternalInspectObjectWithResponse(ctx, req)
	if err != nil {
		return nil, mapInspectError(target, err)
	}
	if resp.JSON200 == nil {
		return nil, mapInspectError(target, apierror.FromResponse(resp.HTTPResponse, resp.Body))
	}
	return &inspectedObject{
		objectURL: resp.JSON200.ObjectUrl,
		info: &sycloud.ObjectInfo{
			Bucket:      resp.JSON200.Bucket,
			Key:         resp.JSON200.Key,
			Path:        resp.JSON200.Path,
			SizeBytes:   resp.JSON200.SizeBytes,
			MetaSHA256:  resp.JSON200.MetaSha256,
			ETag:        resp.JSON200.Etag,
			LastModTime: parseInspectLastModified(resp.JSON200.LastModified),
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
	var respErr *apierror.APIError
	if !errors.As(err, &respErr) {
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
