package transfer

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/calypr/git-drs/internal/globusauth"
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
	method := selectAccessMethod(obj)
	if method == nil || method.AccessId == nil {
		return ""
	}
	return strings.TrimSpace(*method.AccessId)
}

func selectAccessMethod(obj drsapi.DrsObject) *drsapi.AccessMethod {
	if obj.AccessMethods == nil {
		return nil
	}
	methods := *obj.AccessMethods
	preferred := strings.TrimSpace(strings.ToLower(os.Getenv("GIT_DRS_ACCESS_METHOD")))
	if preferred == "" {
		preferred = strings.TrimSpace(strings.ToLower(os.Getenv("GIT_DRS_TRANSFER_PROVIDER")))
	}
	if preferred != "" && preferred != "auto" {
		for i := range methods {
			if strings.EqualFold(string(methods[i].Type), preferred) && accessMethodUsable(methods[i]) {
				return &methods[i]
			}
		}
	}
	for i := range methods {
		if accessMethodUsable(methods[i]) {
			return &methods[i]
		}
	}
	return nil
}

func accessMethodUsable(method drsapi.AccessMethod) bool {
	if !accessMethodHasLocator(method) {
		return false
	}
	if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
		if isGlobusURL(method.AccessUrl.Url) {
			return strings.TrimSpace(os.Getenv(globusauth.TransferTokenEnv)) != "" && strings.TrimSpace(os.Getenv(globusDestCollectionEnv)) != ""
		}
		u, err := url.Parse(strings.TrimSpace(method.AccessUrl.Url))
		return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
	}
	if method.Type != drsapi.AccessMethodTypeGlobus {
		return true
	}
	return strings.TrimSpace(os.Getenv(globusauth.TransferTokenEnv)) != "" && strings.TrimSpace(os.Getenv(globusDestCollectionEnv)) != ""
}

func accessMethodHasLocator(method drsapi.AccessMethod) bool {
	if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
		return true
	}
	return method.AccessId != nil && strings.TrimSpace(*method.AccessId) != ""
}
