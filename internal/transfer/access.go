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

// BulkAccessURLsForObjects plans each object before any transfer begins. DRS
// /access resolution is planning because it may reveal the Globus source used
// for destination routing.
func BulkAccessURLsForObjects(ctx context.Context, drsCtx *remoteruntime.GitContext, objects []drsapi.DrsObject) (map[string]drsapi.AccessURL, error) {
	if drsCtx == nil || drsCtx.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	out := make(map[string]drsapi.AccessURL, len(objects))
	var diagnostics []error
	for _, obj := range objects {
		accessURL, err := planAccessURL(ctx, drsCtx, obj)
		if err != nil {
			diagnostics = append(diagnostics, err)
			continue
		}
		out[strings.TrimSpace(obj.Id)] = *accessURL
	}
	return out, errors.Join(diagnostics...)
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

func planAccessURL(ctx context.Context, drsCtx *remoteruntime.GitContext, obj drsapi.DrsObject) (*drsapi.AccessURL, error) {
	methods, policy, err := orderedAccessMethods(obj, accessPolicyFor(drsCtx))
	if err != nil {
		return nil, err
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

		accessURL, err := accessURLForMethod(ctx, drsCtx, obj.Id, method)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("%s=broken (%v)", methodType, err))
			continue
		}
		state, reason := resolvedAccessReadiness(drsCtx, accessURL.Url)
		if state == globusauth.Ready {
			return accessURL, nil
		}
		diagnostics = append(diagnostics, fmt.Sprintf("%s=%s (%s)", methodType, state, reason))
	}
	if policy.mode == "require" && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, fmt.Sprintf("%s=disabled (not advertised)", policy.method))
	}
	return nil, fmt.Errorf("%w: object %s: no usable access method for %s: %s", ErrAccessMethodSelection, obj.Id, policy.mode, strings.Join(diagnostics, "; "))
}

func accessURLForMethod(ctx context.Context, drsCtx *remoteruntime.GitContext, objectID string, method *drsapi.AccessMethod) (*drsapi.AccessURL, error) {
	if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
		raw := strings.TrimSpace(method.AccessUrl.Url)
		if isHTTPURL(raw) || isGlobusURL(raw) || method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
			return &drsapi.AccessURL{Headers: method.AccessUrl.Headers, Url: raw}, nil
		}
	}
	if method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
		return nil, fmt.Errorf("no access URL or access ID")
	}
	accessURL, err := drsCtx.Client.DRS().GetAccessURL(ctx, objectID, strings.TrimSpace(*method.AccessId))
	if err != nil {
		return nil, fmt.Errorf("DRS /access resolution failed: %w", err)
	}
	return &accessURL, nil
}

func resolvedAccessReadiness(drsCtx *remoteruntime.GitContext, rawURL string) (globusauth.Readiness, string) {
	if isHTTPURL(rawURL) {
		return globusauth.Ready, "HTTP handler available"
	}
	if isGlobusURL(rawURL) {
		source, err := parseGlobusURL(rawURL)
		if err != nil {
			return globusauth.Broken, err.Error()
		}
		if _, _, err := resolveGlobusDestination(drsCtx, source.Collection); err != nil {
			return globusauth.Disabled, err.Error()
		}
		return globusauth.CredentialReadiness()
	}
	return globusauth.Disabled, fmt.Sprintf("no handler for resolved URL %q", rawURL)
}

// selectAccessMethod is retained for callers that only need preliminary
// metadata selection. Transfer planning must use planAccessURL.
func selectAccessMethod(obj drsapi.DrsObject) *drsapi.AccessMethod {
	method, _ := selectAccessMethodWithPolicy(obj, environmentAccessPolicy())
	return method
}

func selectAccessMethodWithPolicy(obj drsapi.DrsObject, rawPolicy string) (*drsapi.AccessMethod, error) {
	methods, policy, err := orderedAccessMethods(obj, rawPolicy)
	if err != nil {
		return nil, err
	}
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

func accessMethodReadiness(method drsapi.AccessMethod) (globusauth.Readiness, string) {
	if method.Available != nil && !*method.Available {
		return globusauth.Disabled, "marked unavailable by server"
	}
	if method.AccessUrl != nil && strings.TrimSpace(method.AccessUrl.Url) != "" {
		return resolvedAccessReadiness(nil, method.AccessUrl.Url)
	}
	if method.AccessId == nil || strings.TrimSpace(*method.AccessId) == "" {
		return globusauth.Broken, "no access URL or access ID"
	}
	if method.Type == drsapi.AccessMethodTypeGlobus {
		return globusauth.CredentialReadiness()
	}
	return globusauth.Ready, "DRS access ID can be resolved"
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}
