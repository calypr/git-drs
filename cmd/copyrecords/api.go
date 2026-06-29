package copyrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/request"
	syservices "github.com/calypr/syfon/client/services"
)

type copyHashInfo map[string]string

type copyRecord struct {
	AccessMethods    *[]drsapi.AccessMethod `json:"access_methods,omitempty"`
	ControlledAccess *[]string              `json:"controlled_access,omitempty"`
	CreatedTime      *string                `json:"created_time,omitempty"`
	Description      *string                `json:"description,omitempty"`
	Did              string                 `json:"did"`
	Name             *string                `json:"name,omitempty"`
	Hashes           *copyHashInfo          `json:"hashes,omitempty"`
	Organization     *string                `json:"organization,omitempty"`
	Project          *string                `json:"project,omitempty"`
	Size             *int64                 `json:"size,omitempty"`
	UpdatedTime      *string                `json:"updated_time,omitempty"`
	Version          *string                `json:"version,omitempty"`
}

type copyListRecordsResponse struct {
	Records *[]copyRecord `json:"records,omitempty"`
}

type copyBulkCreateRequest struct {
	Records []copyRecord `json:"records"`
}

type copyBulkHashesRequest struct {
	Hashes []string `json:"hashes"`
}

type copyBulkHashesResponse struct {
	Results map[string][]copyRecord `json:"results,omitempty"`
}

type indexAPI interface {
	List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error)
	BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error)
	BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error)
	CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error)
}

type rawIndexAPI struct {
	requestor request.Requester
}

func newRawIndexAPI(requestor request.Requester) *rawIndexAPI {
	return &rawIndexAPI{requestor: requestor}
}

func (r *rawIndexAPI) List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error) {
	params := url.Values{}
	if opts.Hash != "" {
		params.Set("hash", opts.Hash)
	}
	if opts.URL != "" {
		params.Set("url", opts.URL)
	}
	if opts.Organization != "" {
		params.Set("organization", opts.Organization)
	}
	if opts.ProjectID != "" {
		params.Set("project", opts.ProjectID)
	}
	if opts.Limit != 0 {
		params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Page != 0 {
		params.Set("page", fmt.Sprintf("%d", opts.Page))
	}
	var out copyListRecordsResponse
	if err := r.requestor.Do(ctx, http.MethodGet, "/index", nil, &out, request.WithQueryValues(params)); err != nil {
		return copyListRecordsResponse{}, err
	}
	return out, nil
}

func (r *rawIndexAPI) BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error) {
	var out []copyRecord
	if err := r.requestor.Do(ctx, http.MethodPost, "/index/bulk/documents", dids, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *rawIndexAPI) BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error) {
	var out copyBulkHashesResponse
	if err := r.requestor.Do(ctx, http.MethodPost, "/index/bulk/hashes", copyBulkHashesRequest{Hashes: hashes}, &out); err != nil {
		return copyBulkHashesResponse{}, err
	}
	return out, nil
}

func (r *rawIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	var out copyListRecordsResponse
	if err := r.requestor.Do(ctx, http.MethodPost, "/index/bulk", req, &out); err != nil {
		return copyListRecordsResponse{}, err
	}
	return out, nil
}

func canonicalAccessMethod(method drsapi.AccessMethod) string {
	b, err := json.Marshal(method)
	if err != nil {
		return fmt.Sprintf("%s|%v", method.Type, method.AccessId)
	}
	return string(b)
}
