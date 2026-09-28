package transfer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
)

var ErrAccessMethodSelection = errors.New("access method selection failed")

// ResolvedAccess keeps the URL, request headers, and the access method that
// produced them together. The access ID is required for refreshing an expired
// URL; it must never be inferred from the object's method ordering.
type ResolvedAccess struct {
	AccessURL drsapi.AccessURL
	AccessID  string
}

// BulkAccessURLsForObjects plans each object before any transfer begins. DRS
// /access resolution is planning because it may reveal the Globus source used
// for destination routing.
func BulkResolvedAccessURLsForObjects(ctx context.Context, drsCtx *remoteruntime.GitContext, objects []drsapi.DrsObject) (map[string]ResolvedAccess, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	out := make(map[string]ResolvedAccess, len(objects))
	var diagnostics []error
	bulkPlans, ok := bulkAccessRequest(drsCtx, objects)
	if ok {
		if api := drsCtx.Client.DRSAPI(); api != nil {
			if resp, err := api.GetBulkAccessURLWithResponse(ctx, bulkPlans.request); err == nil && resp.JSON200 != nil && resp.JSON200.ResolvedDrsObjectAccessUrls != nil {
				candidates := make(map[string]map[string][]ResolvedAccess)
				for _, resolved := range *resp.JSON200.ResolvedDrsObjectAccessUrls {
					if resolved.DrsObjectId == nil || resolved.DrsAccessId == nil || strings.TrimSpace(*resolved.DrsObjectId) == "" || strings.TrimSpace(*resolved.DrsAccessId) == "" || strings.TrimSpace(resolved.Url) == "" {
						continue
					}
					objectID := strings.TrimSpace(*resolved.DrsObjectId)
					accessID := strings.TrimSpace(*resolved.DrsAccessId)
					_, requested := bulkPlans.byObject[objectID][accessID]
					if !requested {
						continue
					}
					accessURL := drsapi.AccessURL{Headers: cloneAccessHeaders(resolved.Headers), Url: strings.TrimSpace(resolved.Url)}
					if state, _ := resolvedAccessReadiness(drsCtx, accessURL.Url); state != globusauth.Ready {
						continue
					}
					if candidates[objectID] == nil {
						candidates[objectID] = make(map[string][]ResolvedAccess)
					}
					candidates[objectID][accessID] = append(candidates[objectID][accessID], ResolvedAccess{AccessURL: accessURL, AccessID: accessID})
				}
				for objectID, byAccessID := range candidates {
					for accessID, matches := range byAccessID {
						// A duplicate result for one requested method is ambiguous.
						// Fall back to the ordinary per-object planner instead of
						// allowing server response order to select the URL.
						if len(matches) != 1 {
							continue
						}
						rank := bulkPlans.byObject[objectID][accessID]
						if _, exists := out[objectID]; !exists || rank < bulkPlans.rank[objectID] {
							out[objectID] = matches[0]
							bulkPlans.rank[objectID] = rank
						}
					}
				}
			}
		}
	}
	for _, obj := range objects {
		if _, ok := out[strings.TrimSpace(obj.Id)]; ok {
			continue
		}
		resolvedAccess, err := planResolvedAccess(ctx, drsCtx, obj)
		if err != nil {
			diagnostics = append(diagnostics, err)
			continue
		}
		out[strings.TrimSpace(obj.Id)] = resolvedAccess
	}
	return out, errors.Join(diagnostics...)
}

// BulkAccessURLsForObjects is retained as a leaf compatibility adapter. New
// transfer paths must use BulkResolvedAccessURLsForObjects so refresh retains
// the exact access ID and headers selected during planning.
func BulkAccessURLsForObjects(ctx context.Context, drsCtx *remoteruntime.GitContext, objects []drsapi.DrsObject) (map[string]drsapi.AccessURL, error) {
	resolved, err := BulkResolvedAccessURLsForObjects(ctx, drsCtx, objects)
	if resolved == nil {
		return nil, err
	}
	out := make(map[string]drsapi.AccessURL, len(resolved))
	for objectID, access := range resolved {
		out[objectID] = access.AccessURL
	}
	return out, err
}

type bulkAccessPlan struct {
	request  drsapi.BulkObjectAccessId
	byObject map[string]map[string]int
	rank     map[string]int
}

func bulkAccessRequest(drsCtx *remoteruntime.GitContext, objects []drsapi.DrsObject) (bulkAccessPlan, bool) {
	plan := bulkAccessPlan{byObject: make(map[string]map[string]int), rank: make(map[string]int)}
	items := make([]struct {
		BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
		BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
	}, 0, len(objects))
	for _, obj := range objects {
		objectID := strings.TrimSpace(obj.Id)
		if objectID == "" {
			continue
		}
		methods, policy, err := orderedAccessMethods(obj, accessPolicyFor(drsCtx))
		if err != nil {
			continue
		}
		accessIDs := make([]string, 0, len(methods))
		byAccessID := make(map[string]int)
		for methodIndex, method := range methods {
			methodType := strings.ToLower(string(method.Type))
			if policy.mode == "require" && methodType != policy.method {
				continue
			}
			if method.Available != nil && !*method.Available {
				continue
			}
			if methodType == "globus" {
				state, _ := globusauth.CredentialReadiness()
				if state != globusauth.Ready {
					continue
				}
			}
			accessID := ""
			if method.AccessId != nil {
				accessID = strings.TrimSpace(*method.AccessId)
			}
			if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
				raw := strings.TrimSpace(method.AccessUrl.Url)
				if isHTTPURL(raw) || isGlobusURL(raw) || accessID == "" {
					if state, _ := resolvedAccessReadiness(drsCtx, raw); state == globusauth.Ready {
						// The ordinary planner would choose this embedded URL before
						// any later access ID, so this object cannot use bulk planning.
						accessIDs = nil
						break
					}
					continue
				}
			}
			if method.AccessUrl == nil && accessID != "" && !supportedAccessMethodType(methodType) {
				continue
			}
			if method.AccessId == nil {
				continue
			}
			if accessID == "" {
				continue
			}
			if _, duplicate := byAccessID[accessID]; duplicate {
				continue
			}
			byAccessID[accessID] = methodIndex
			accessIDs = append(accessIDs, accessID)
		}
		if len(accessIDs) == 0 {
			continue
		}
		items = append(items, struct {
			BulkAccessIds *[]string `json:"bulk_access_ids,omitempty"`
			BulkObjectId  *string   `json:"bulk_object_id,omitempty"`
		}{BulkAccessIds: &accessIDs, BulkObjectId: &objectID})
		plan.byObject[objectID] = byAccessID
		plan.rank[objectID] = len(methods) + 1
	}
	if len(items) == 0 {
		return plan, false
	}
	plan.request.BulkObjectAccessIds = &items
	return plan, true
}

func supportedAccessMethodType(methodType string) bool {
	switch methodType {
	case "https", "s3", "gs", "globus":
		return true
	default:
		return false
	}
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
		return accessPolicy{mode: "prefer", method: raw}, nil
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

func orderedAccessMethods(obj drsapi.DrsObject, rawPolicy string) ([]drsapi.AccessMethod, accessPolicy, error) {
	if obj.AccessMethods == nil {
		return nil, accessPolicy{}, fmt.Errorf("%w: object %s: no access methods advertised", ErrAccessMethodSelection, obj.Id)
	}
	policy, err := parseAccessPolicy(rawPolicy)
	if err != nil {
		return nil, policy, fmt.Errorf("%w: %v", ErrAccessMethodSelection, err)
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
	return methods, policy, nil
}

func planResolvedAccess(ctx context.Context, drsCtx *remoteruntime.GitContext, obj drsapi.DrsObject) (ResolvedAccess, error) {
	methods, policy, err := orderedAccessMethods(obj, accessPolicyFor(drsCtx))
	if err != nil {
		return ResolvedAccess{}, err
	}
	diagnostics := make([]string, 0, len(methods))
	for i := range methods {
		method := &methods[i]
		methodType := strings.ToLower(string(method.Type))
		if policy.mode == "require" && methodType != policy.method {
			continue
		}
		if method.Available != nil && !*method.Available {
			diagnostics = append(diagnostics, methodType+"=disabled (marked unavailable by server)")
			continue
		}
		if methodType == "globus" {
			state, reason := globusauth.CredentialReadiness()
			if state != globusauth.Ready {
				diagnostics = append(diagnostics, fmt.Sprintf("globus=%s (%s)", state, reason))
				continue
			}
		}
		if method.AccessUrl == nil && method.AccessId != nil && methodType != "https" && methodType != "s3" && methodType != "gs" && methodType != "globus" {
			diagnostics = append(diagnostics, methodType+"=disabled (unsupported access method type)")
			continue
		}

		resolved, err := accessURLForMethod(ctx, drsCtx, obj.Id, method)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s=broken (%v)", methodType, err))
			continue
		}
		state, reason := resolvedAccessReadiness(drsCtx, resolved.AccessURL.Url)
		if state == globusauth.Ready {
			return resolved, nil
		}
		diagnostics = append(diagnostics, fmt.Sprintf("%s=%s (%s)", methodType, state, reason))
	}
	if policy.mode == "require" && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s=disabled (not advertised)", policy.method))
	}
	return ResolvedAccess{}, fmt.Errorf("%w: object %s: no usable access method for %s: %s", ErrAccessMethodSelection, obj.Id, policy.mode, strings.Join(diagnostics, "; "))
}

func planAccessURL(ctx context.Context, drsCtx *remoteruntime.GitContext, obj drsapi.DrsObject) (*drsapi.AccessURL, error) {
	resolved, err := planResolvedAccess(ctx, drsCtx, obj)
	if err != nil {
		return nil, err
	}
	return &resolved.AccessURL, nil
}

func accessURLForMethod(ctx context.Context, drsCtx *remoteruntime.GitContext, objectID string, method *drsapi.AccessMethod) (ResolvedAccess, error) {
	accessID := ""
	if method.AccessId != nil {
		accessID = strings.TrimSpace(*method.AccessId)
	}
	if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
		raw := strings.TrimSpace(method.AccessUrl.Url)
		if isHTTPURL(raw) || isGlobusURL(raw) || method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
			return ResolvedAccess{AccessURL: cloneAccessURL(*method.AccessUrl, raw), AccessID: accessID}, nil
		}
	}
	if method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
		return ResolvedAccess{}, fmt.Errorf("no access URL or access ID")
	}
	accessURL, err := drsCtx.Client.DRS().GetAccessURL(ctx, objectID, strings.TrimSpace(*method.AccessId))
	if err != nil {
		return ResolvedAccess{}, fmt.Errorf("DRS /access resolution failed: %w", err)
	}
	return ResolvedAccess{AccessURL: cloneAccessURL(accessURL, strings.TrimSpace(accessURL.Url)), AccessID: accessID}, nil
}

func cloneAccessHeaders(headers *[]string) *[]string {
	if headers == nil {
		return nil
	}
	copyHeaders := append([]string(nil), (*headers)...)
	return &copyHeaders
}

func cloneAccessURL(accessURL drsapi.AccessURL, resolvedURL string) drsapi.AccessURL {
	accessURL.Url = resolvedURL
	accessURL.Headers = cloneAccessHeaders(accessURL.Headers)
	return accessURL
}

func resolvedAccessReadiness(drsCtx *remoteruntime.GitContext, rawURL string) (globusauth.Readiness, string) {
	if isHTTPURL(rawURL) {
		return globusauth.Ready, "HTTP handler available"
	}
	if isGlobusURL(rawURL) {
		if drsCtx != nil && drsCtx.LFSObjectsRoot != "" && drsCtx.RepositoryRoot != "" {
			if err := globusStorageRepresentationError(drsCtx.LFSObjectsRoot, drsCtx.RepositoryRoot); err != nil {
				return globusauth.Disabled, err.Error()
			}
		}
		source, err := parseGlobusURL(rawURL)
		if err != nil {
			return globusauth.Broken, err.Error()
		}
		if _, _, err := resolveGlobusDestination(drsCtx, source.Collection); err != nil {
			return globusauth.Disabled, err.Error()
		}
		return globusauth.CredentialReadiness()
	}
	if isLocalFileURL(rawURL) {
		if drsCtx != nil && drsCtx.RemoteType == config.LocalServerType {
			return globusauth.Ready, "local file handler available"
		}
		return globusauth.Disabled, "filesystem access is allowed only for local remotes"
	}
	return globusauth.Disabled, fmt.Sprintf("no handler for resolved URL %q", rawURL)
}

func accessMethodRank(method string) int {
	for rank, known := range []string{"https", "s3", "gs", "globus"} {
		if method == known {
			return rank
		}
	}
	return 4
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && u.Host != "" && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

func isLocalFileURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	return err == nil && filepath.IsAbs(u.Path) && (u.Scheme == "" || strings.EqualFold(u.Scheme, "file"))
}
