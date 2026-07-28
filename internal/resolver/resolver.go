// Package resolver provides provider-neutral DRS metadata and access resolution.
package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var (
	ErrCredentials  = errors.New("google application default credentials are unavailable")
	ErrUnauthorized = errors.New("not authorized to access AnVIL DRS object")
	ErrNotFound     = errors.New("AnVIL DRS object not found")
)

type Resolver interface {
	GetObject(context.Context, string) (*ResolvedObject, error)
	GetAccess(context.Context, string, string) (*ResolvedAccess, error)
}

// DownloadToCache resolves a fresh access URL, streams into a temporary file,
// and atomically promotes it. Neither the URL nor its headers are persisted.
func DownloadToCache(ctx context.Context, r Resolver, drsURI, destination string) error {
	obj, err := r.GetObject(ctx, drsURI)
	if err != nil {
		return err
	}
	var access *ResolvedAccess
	for _, method := range obj.AccessMethods {
		switch {
		case method.AccessURL != nil && strings.TrimSpace(method.AccessURL.URL) != "":
			access = method.AccessURL
		case strings.TrimSpace(method.AccessID) != "":
			access, err = r.GetAccess(ctx, drsURI, method.AccessID)
			if err != nil {
				return err
			}
		}
		if access != nil {
			break
		}
	}
	if access == nil {
		return fmt.Errorf("AnVIL DRS object has no supported access method")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".git-drs-download-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, access.URL, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	for _, header := range access.Headers {
		key, value, ok := strings.Cut(header, ":")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			tmp.Close()
			return fmt.Errorf("AnVIL resolver returned an invalid access header")
		}
		req.Header.Add(key, strings.TrimSpace(value))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("AnVIL data download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tmp.Close()
		return fmt.Errorf("AnVIL data download returned HTTP %d", resp.StatusCode)
	}
	_, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		return fmt.Errorf("AnVIL data download interrupted: %w", copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if obj.Size >= 0 {
		info, statErr := os.Stat(tmpName)
		if statErr != nil {
			return statErr
		}
		if info.Size() != obj.Size {
			return fmt.Errorf("AnVIL data size mismatch: expected %d, got %d", obj.Size, info.Size())
		}
	}
	return os.Rename(tmpName, destination)
}

type AccessMethod struct {
	Type      string          `json:"type"`
	AccessID  string          `json:"access_id,omitempty"`
	AccessURL *ResolvedAccess `json:"access_url,omitempty"`
}

type ResolvedObject struct {
	DRSURI        string
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Size          int64          `json:"size"`
	Checksums     []Checksum     `json:"checksums"`
	AccessMethods []AccessMethod `json:"access_methods"`
}

type Checksum struct {
	Type     string `json:"type"`
	Checksum string `json:"checksum"`
}

type ResolvedAccess struct {
	URL     string   `json:"url"`
	Headers []string `json:"headers"`
}

// AnVILResolver routes every DRS authority through the configured trusted
// Terra/AnVIL resolver endpoint. Authorization is attached only by the HTTP
// client created for that endpoint; access URLs are returned ephemerally.
type AnVILResolver struct {
	endpoint *url.URL
	client   *http.Client
}

func NewAnVIL(ctx context.Context, endpoint string) (*AnVILResolver, error) {
	u, err := trustedEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("%w: run `gcloud auth application-default login`: %v", ErrCredentials, err)
	}
	return &AnVILResolver{endpoint: u, client: &http.Client{Transport: &oauth2.Transport{Base: http.DefaultTransport, Source: creds.TokenSource}}}, nil
}

// NewAnVILWithClient supports contract tests and callers that already own an
// authenticated, refreshing client.
func NewAnVILWithClient(endpoint string, client *http.Client) (*AnVILResolver, error) {
	u, err := trustedEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}
	return &AnVILResolver{endpoint: u, client: client}, nil
}

func trustedEndpoint(endpoint string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, fmt.Errorf("invalid AnVIL resolver endpoint")
	}
	return u, nil
}

func NormalizeDRSURI(raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	// DRS compact identifiers use a colon between the authority and object ID
	// (for example, drs://drs.anv0:v2_...). net/url interprets that colon as
	// the beginning of a port and rejects the URI before we can extract the ID.
	if len(trimmed) >= len("drs://") && strings.EqualFold(trimmed[:len("drs://")], "drs://") {
		remainder := trimmed[len("drs://"):]
		if separator := strings.IndexByte(remainder, ':'); separator >= 0 && !strings.Contains(remainder[:separator], "/") {
			authority, escapedID := remainder[:separator], remainder[separator+1:]
			authorityURL, authorityErr := url.Parse("https://" + authority)
			id, idErr := url.PathUnescape(escapedID)
			if authorityErr != nil || authorityURL.Host != authority || authorityURL.Hostname() == "" || authorityURL.User != nil ||
				strings.ContainsAny(escapedID, "?#/") {
				return "", "", fmt.Errorf("invalid DRS URI %q", raw)
			}
			if idErr != nil || strings.TrimSpace(id) == "" {
				return "", "", fmt.Errorf("invalid DRS URI %q: object ID is required", raw)
			}
			return "drs://" + strings.ToLower(authority) + ":" + escapedID, id, nil
		}
	}

	u, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(u.Scheme, "drs") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("invalid DRS URI %q", raw)
	}
	id, err := url.PathUnescape(strings.TrimPrefix(u.EscapedPath(), "/"))
	if err != nil || strings.TrimSpace(id) == "" {
		return "", "", fmt.Errorf("invalid DRS URI %q: object ID is required", raw)
	}
	u.Scheme = "drs"
	u.Host = strings.ToLower(u.Host)
	return u.String(), id, nil
}

func (r *AnVILResolver) GetObject(ctx context.Context, drsURI string) (*ResolvedObject, error) {
	canonical, id, err := NormalizeDRSURI(drsURI)
	if err != nil {
		return nil, err
	}
	var obj ResolvedObject
	if err := r.getJSON(ctx, r.apiURL("objects", id), &obj); err != nil {
		return nil, err
	}
	obj.DRSURI = canonical
	if obj.ID == "" {
		obj.ID = id
	}
	return &obj, nil
}

func (r *AnVILResolver) GetAccess(ctx context.Context, drsURI, accessID string) (*ResolvedAccess, error) {
	_, id, err := NormalizeDRSURI(drsURI)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(accessID) == "" {
		return nil, fmt.Errorf("access ID is required")
	}
	var access ResolvedAccess
	if err := r.getJSON(ctx, r.apiURL("objects", id, "access", accessID), &access); err != nil {
		return nil, err
	}
	if strings.TrimSpace(access.URL) == "" {
		return nil, fmt.Errorf("AnVIL resolver returned an empty access URL")
	}
	return &access, nil
}

func (r *AnVILResolver) apiURL(parts ...string) string {
	u := *r.endpoint
	segments := append([]string{u.Path, "ga4gh", "drs", "v1"}, parts...)
	u.Path = path.Join(segments...)
	return u.String()
}

func (r *AnVILResolver) getJSON(ctx context.Context, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("AnVIL resolver unavailable: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("AnVIL resolver returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(dst); err != nil {
		return fmt.Errorf("decode AnVIL resolver response: %w", err)
	}
	return nil
}
