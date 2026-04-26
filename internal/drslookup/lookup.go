package drslookup

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func ObjectsByHash(ctx context.Context, drsCtx *config.GitContext, checksum string) ([]drsapi.DrsObject, error) {
	if drsCtx == nil || drsCtx.Requestor == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	checksum = normalizeChecksum(checksum)
	if checksum == "" {
		return nil, nil
	}
	var out drsapi.N200OkDrsObjects
	path := "/ga4gh/drs/v1/objects/checksum/" + url.PathEscape(checksum)
	if err := drsCtx.Requestor.Do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	if out.ResolvedDrsObject == nil {
		return nil, nil
	}
	return *out.ResolvedDrsObject, nil
}

func ObjectsByHashForScope(ctx context.Context, drsCtx *config.GitContext, checksum string) ([]drsapi.DrsObject, error) {
	objects, err := ObjectsByHash(ctx, drsCtx, checksum)
	if err != nil {
		return nil, err
	}
	result := make([]drsapi.DrsObject, 0, len(objects))
	for _, obj := range objects {
		if MatchesScope(&obj, drsCtx.Organization, drsCtx.ProjectId) {
			result = append(result, obj)
		}
	}
	return result, nil
}

func AccessURLForHashScope(ctx context.Context, drsCtx *config.GitContext, checksum string) (*drsapi.AccessURL, *drsapi.DrsObject, error) {
	records, err := ObjectsByHashForScope(ctx, drsCtx, checksum)
	if err != nil {
		return nil, nil, err
	}
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("no matching DRS record found for oid %s", normalizeChecksum(checksum))
	}
	match := records[0]
	if match.AccessMethods == nil || len(*match.AccessMethods) == 0 {
		return nil, nil, fmt.Errorf("no access methods available for DRS object %s", match.Id)
	}
	accessType := (*match.AccessMethods)[0].Type
	if accessType == "" {
		return nil, nil, fmt.Errorf("no access type found in access method for DRS object %s", match.Id)
	}
	accessURL, err := drsCtx.Client.DRS().GetAccessURL(ctx, match.Id, string(accessType))
	if err != nil {
		return nil, nil, err
	}
	return &accessURL, &match, nil
}

func BulkAccessURLsForObjects(ctx context.Context, drsCtx *config.GitContext, objects []drsapi.DrsObject) (map[string]drsapi.AccessURL, error) {
	if drsCtx == nil || drsCtx.Requestor == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}

	type bulkObjectAccessID struct {
		BulkObjectID  string   `json:"bulk_object_id"`
		BulkAccessIDs []string `json:"bulk_access_ids"`
	}
	req := struct {
		BulkObjectAccessIDs []bulkObjectAccessID `json:"bulk_object_access_ids"`
	}{}

	for _, obj := range objects {
		accessID := firstAccessID(obj)
		if strings.TrimSpace(obj.Id) == "" || accessID == "" {
			continue
		}
		req.BulkObjectAccessIDs = append(req.BulkObjectAccessIDs, bulkObjectAccessID{
			BulkObjectID: strings.TrimSpace(obj.Id),
			BulkAccessIDs: []string{
				accessID,
			},
		})
	}
	if len(req.BulkObjectAccessIDs) == 0 {
		return map[string]drsapi.AccessURL{}, nil
	}

	var resp struct {
		ResolvedDrsObjectAccessURLs []drsapi.BulkAccessURL `json:"resolved_drs_object_access_urls"`
	}
	if err := drsCtx.Requestor.Do(ctx, http.MethodPost, "/ga4gh/drs/v1/objects/access", req, &resp); err != nil {
		return nil, err
	}

	out := make(map[string]drsapi.AccessURL, len(resp.ResolvedDrsObjectAccessURLs))
	for _, resolved := range resp.ResolvedDrsObjectAccessURLs {
		objectID := strings.TrimSpace(stringPtrValue(resolved.DrsObjectId))
		if objectID == "" || strings.TrimSpace(resolved.Url) == "" {
			continue
		}
		out[objectID] = drsapi.AccessURL{
			Headers: resolved.Headers,
			Url:     resolved.Url,
		}
	}
	return out, nil
}

func firstAccessID(obj drsapi.DrsObject) string {
	if obj.AccessMethods == nil || len(*obj.AccessMethods) == 0 {
		return ""
	}
	method := (*obj.AccessMethods)[0]
	if method.AccessId != nil && strings.TrimSpace(*method.AccessId) != "" {
		return strings.TrimSpace(*method.AccessId)
	}
	return strings.TrimSpace(string(method.Type))
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func MatchesScope(obj *drsapi.DrsObject, organization, project string) bool {
	if obj == nil || obj.AccessMethods == nil {
		return false
	}
	for _, method := range *obj.AccessMethods {
		if method.Authorizations == nil {
			continue
		}
		if common.AuthzMatchesScope(*method.Authorizations, organization, project) {
			return true
		}
	}
	return false
}

func normalizeChecksum(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "sha256:")
	return strings.TrimSpace(raw)
}
