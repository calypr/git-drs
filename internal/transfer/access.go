package transfer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

var ErrAccessMethodSelection = errors.New("access method selection failed")

func BulkAccessURLsForObjects(ctx context.Context, drsCtx *remoteruntime.GitContext, objects []drsapi.DrsObject) (map[string]drsapi.AccessURL, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	req, ok, err := bulkAccessRequest(objects, accessPolicyFor(drsCtx))
	if err != nil {
		return nil, err
	}
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

func bulkAccessRequest(objects []drsapi.DrsObject, policy string) (drsapi.BulkObjectAccessId, bool, error) {
	req := drsapi.BulkObjectAccessId{}
	var diagnostics []error
	items := make([]struct {
		BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
		BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
	}, 0, len(objects))

	for _, obj := range objects {
		objectID := strings.TrimSpace(obj.Id)
		accessID, err := accessIDForBulkRequest(obj, policy)
		if err != nil {
			diagnostics = append(diagnostics, err)
			continue
		}
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
		return req, false, errors.Join(diagnostics...)
	}
	req.BulkObjectAccessIds = &items
	return req, true, errors.Join(diagnostics...)
}

func accessIDForBulkRequest(obj drsapi.DrsObject, policy string) (string, error) {
	method, err := selectAccessMethodWithPolicy(obj, policy)
	if err != nil {
		return "", err
	}
	if method == nil || method.AccessId == nil {
		return "", nil
	}
	return strings.TrimSpace(*method.AccessId), nil
}

func selectAccessMethod(obj drsapi.DrsObject) *drsapi.AccessMethod {
	method, _ := selectAccessMethodWithPolicy(obj, environmentAccessPolicy())
	return method
}

type accessPolicy struct {
	mode, method string
}

func parseAccessPolicy(raw string) (accessPolicy, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || raw == "auto" {
		return accessPolicy{mode: "auto"}, nil
	}
	mode, method, found := strings.Cut(raw, ":")
	if !found {
		return accessPolicy{mode: "prefer", method: raw}, nil // backward-compatible environment/config form
	}
	if (mode != "prefer" && mode != "require") || strings.TrimSpace(method) == "" {
		return accessPolicy{}, fmt.Errorf("invalid access-method policy %q; use auto, prefer:<type>, or require:<type>", raw)
	}
	return accessPolicy{mode: mode, method: strings.TrimSpace(method)}, nil
}

func environmentAccessPolicy() string {
	if value := strings.TrimSpace(os.Getenv("GIT_DRS_ACCESS_METHOD")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("GIT_DRS_TRANSFER_PROVIDER"))
}

func accessPolicyFor(ctx *remoteruntime.GitContext) string {
	if ctx != nil && strings.TrimSpace(ctx.CommandAccessMethod) != "" {
		return "require:" + strings.TrimSpace(ctx.CommandAccessMethod)
	}
	if value := environmentAccessPolicy(); value != "" {
		return value
	}
	if ctx != nil {
		return ctx.AccessMethodPolicy
	}
	return "auto"
}

func selectAccessMethodWithPolicy(obj drsapi.DrsObject, rawPolicy string) (*drsapi.AccessMethod, error) {
	if obj.AccessMethods == nil {
		return nil, fmt.Errorf("%w: object %s: no access methods advertised", ErrAccessMethodSelection, obj.Id)
	}
	policy, err := parseAccessPolicy(rawPolicy)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAccessMethodSelection, err)
	}
	methods := append([]drsapi.AccessMethod(nil), (*obj.AccessMethods)...)
	sort.SliceStable(methods, func(i, j int) bool {
		left, right := strings.ToLower(string(methods[i].Type)), strings.ToLower(string(methods[j].Type))
		if policy.method != "" {
			leftPreferred, rightPreferred := left == policy.method, right == policy.method
			if leftPreferred != rightPreferred {
				return leftPreferred
			}
		}
		leftRank, rightRank := accessMethodRank(left), accessMethodRank(right)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return left < right
	})
	diagnostics := make([]string, 0, len(methods))
	for i := range methods {
		methodType := strings.ToLower(string(methods[i].Type))
		if policy.mode == "require" && methodType != policy.method {
			continue
		}
		state, reason := accessMethodReadiness(methods[i])
		if state == globusauth.Ready {
			return &methods[i], nil
		}
		diagnostics = append(diagnostics, fmt.Sprintf("%s=%s (%s)", methodType, state, reason))
	}
	if policy.mode == "require" && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s=disabled (not advertised)", policy.method))
	}
	return nil, fmt.Errorf("%w: object %s: no usable access method for %s: %s", ErrAccessMethodSelection, obj.Id, policy.mode, strings.Join(diagnostics, "; "))
}

func accessMethodRank(method string) int {
	for rank, known := range []string{"https", "s3", "gs", "globus"} {
		if method == known {
			return rank
		}
	}
	return 4
}

func accessMethodUsable(method drsapi.AccessMethod) bool {
	state, _ := accessMethodReadiness(method)
	return state == globusauth.Ready
}

func accessMethodReadiness(method drsapi.AccessMethod) (globusauth.Readiness, string) {
	if method.Available != nil && !*method.Available {
		return globusauth.Disabled, "marked unavailable by server"
	}
	if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
		if isGlobusURL(method.AccessUrl.Url) {
			return globusReadiness()
		}
		if isHTTPURL(method.AccessUrl.Url) {
			return globusauth.Ready, "HTTP handler available"
		}
	}
	if method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
		return globusauth.Broken, "no supported access URL or access ID"
	}
	if method.Type != drsapi.AccessMethodTypeGlobus {
		return globusauth.Ready, "DRS access ID can be resolved"
	}
	return globusReadiness()
}

func globusReadiness() (globusauth.Readiness, string) {
	if strings.TrimSpace(os.Getenv(globusDestCollectionEnv)) == "" {
		return globusauth.Disabled, fmt.Sprintf("%s is not configured", globusDestCollectionEnv)
	}
	return globusauth.CredentialReadiness()
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}
