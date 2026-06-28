package drsdownload

import (
	"context"
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func BulkAccessURLsForObjects(ctx context.Context, drsCtx *remoteruntime.GitContext, objects []drsapi.DrsObject) (map[string]drsapi.AccessURL, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	req, ok := bulkAccessRequest(objects)
	if !ok {
		return map[string]drsapi.AccessURL{}, nil
	}

	resp, err := drsCtx.Client.DRSAPI().GetBulkAccessURLWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, fmt.Errorf("unexpected response: %d", resp.StatusCode())
	}

	out := map[string]drsapi.AccessURL{}
	if resp.JSON200.ResolvedDrsObjectAccessUrls == nil {
		return out, nil
	}
	for _, resolved := range *resp.JSON200.ResolvedDrsObjectAccessUrls {
		if resolved.DrsObjectId == nil {
			continue
		}
		objectID := *resolved.DrsObjectId
		if strings.TrimSpace(objectID) == "" || strings.TrimSpace(resolved.Url) == "" {
			continue
		}
		out[strings.TrimSpace(objectID)] = drsapi.AccessURL{Headers: resolved.Headers, Url: resolved.Url}
	}
	return out, nil
}

func bulkAccessRequest(objects []drsapi.DrsObject) (drsapi.BulkObjectAccessId, bool) {
	req := drsapi.BulkObjectAccessId{}
	items := make([]struct {
		BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
		BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
	}, 0, len(objects))

	for _, obj := range objects {
		objectID := strings.TrimSpace(obj.Id)
		accessID := accessIDForBulkRequest(obj)
		if objectID == "" || accessID == "" {
			continue
		}
		objID := objectID
		accessIDs := []string{accessID}
		items = append(items, struct {
			BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
			BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
		}{
			BulkAccessIds: &accessIDs,
			BulkObjectId:  &objID,
		})
	}
	if len(items) == 0 {
		return req, false
	}
	req.BulkObjectAccessIds = &items
	return req, true
}

func accessIDForBulkRequest(obj drsapi.DrsObject) string {
	if obj.AccessMethods == nil {
		return ""
	}
	for _, method := range *obj.AccessMethods {
		if method.AccessId != nil && strings.TrimSpace(*method.AccessId) != "" {
			return strings.TrimSpace(*method.AccessId)
		}
	}
	return ""
}
