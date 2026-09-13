package copyrecords

import (
	"context"
	"encoding/json"
	"fmt"

	drsapi "github.com/calypr/syfon/apigen/drs"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	"github.com/calypr/syfon/client/apierror"
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

type copyBulkOverwriteRequest struct {
	Organization string       `json:"organization"`
	Project      string       `json:"project"`
	Records      []copyRecord `json:"records"`
}

type copyBulkHashesResponse struct {
	Results map[string][]copyRecord `json:"results,omitempty"`
}

type copyBulkOverwriteResponse struct {
	Processed       int `json:"processed"`
	Created         int `json:"created"`
	Replaced        int `json:"replaced"`
	DIDMatched      int `json:"did_matched"`
	ChecksumMatched int `json:"checksum_matched"`
}

type indexAPI interface {
	List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error)
	BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error)
	BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error)
	CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error)
	OverwriteBulk(ctx context.Context, req copyBulkOverwriteRequest) (copyBulkOverwriteResponse, error)
}

type rawIndexAPI struct {
	client *internalapi.ClientWithResponses
}

func newRawIndexAPI(client *internalapi.ClientWithResponses) *rawIndexAPI {
	return &rawIndexAPI{client: client}
}

func (r *rawIndexAPI) List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error) {
	params := &internalapi.InternalListParams{}
	if opts.Hash != "" {
		params.Hash = &opts.Hash
	}
	if opts.URL != "" {
		params.Url = &opts.URL
	}
	if opts.Organization != "" {
		params.Organization = &opts.Organization
	}
	if opts.ProjectID != "" {
		params.Project = &opts.ProjectID
	}
	if opts.Limit != 0 {
		params.Limit = &opts.Limit
	}
	if opts.Start != "" {
		params.Start = &opts.Start
	} else if opts.Page != 0 {
		params.Page = &opts.Page
	}
	resp, err := r.client.InternalListWithResponse(ctx, params)
	if err != nil {
		return copyListRecordsResponse{}, err
	}
	if resp.JSON200 == nil {
		return copyListRecordsResponse{}, apierror.FromResponse(resp.HTTPResponse, resp.Body)
	}
	return copyListResponse(*resp.JSON200), nil
}

func (r *rawIndexAPI) BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error) {
	var body internalapi.BulkDocumentsRequest
	if err := body.FromBulkDocumentsRequest0(dids); err != nil {
		return nil, err
	}
	resp, err := r.client.InternalBulkDocumentsWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 == nil {
		return nil, apierror.FromResponse(resp.HTTPResponse, resp.Body)
	}
	out := make([]copyRecord, 0, len(*resp.JSON200))
	for _, record := range *resp.JSON200 {
		out = append(out, copyRecordFromResponse(record))
	}
	return out, nil
}

func (r *rawIndexAPI) BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error) {
	resp, err := r.client.InternalBulkHashesWithResponse(ctx, internalapi.BulkHashesRequest{Hashes: hashes})
	if err != nil {
		return copyBulkHashesResponse{}, err
	}
	if resp.JSON200 == nil {
		return copyBulkHashesResponse{}, apierror.FromResponse(resp.HTTPResponse, resp.Body)
	}
	results := make(map[string][]copyRecord, len(hashes))
	for _, hash := range hashes {
		results[hash] = nil
	}
	for hash, records := range resp.JSON200.Results {
		converted := make([]copyRecord, 0, len(records))
		for _, record := range records {
			converted = append(converted, copyRecordFromInternal(record))
		}
		results[hash] = converted
	}
	return copyBulkHashesResponse{Results: results}, nil
}

func (r *rawIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	records := make([]internalapi.InternalRecord, 0, len(req.Records))
	for _, record := range req.Records {
		records = append(records, internalRecordFromCopyRecord(record))
	}
	resp, err := r.client.InternalBulkCreateWithResponse(ctx, internalapi.BulkCreateRequest{Records: records})
	if err != nil {
		return copyListRecordsResponse{}, err
	}
	if resp.JSON201 == nil {
		return copyListRecordsResponse{}, apierror.FromResponse(resp.HTTPResponse, resp.Body)
	}
	return copyListResponse(*resp.JSON201), nil
}

func (r *rawIndexAPI) OverwriteBulk(ctx context.Context, req copyBulkOverwriteRequest) (copyBulkOverwriteResponse, error) {
	records := make([]internalapi.InternalRecord, 0, len(req.Records))
	for _, record := range req.Records {
		records = append(records, internalRecordFromCopyRecord(record))
	}
	resp, err := r.client.InternalBulkOverwriteWithResponse(ctx, internalapi.BulkOverwriteRequest{
		Organization: req.Organization,
		Project:      req.Project,
		Records:      records,
	})
	if err != nil {
		return copyBulkOverwriteResponse{}, err
	}
	if resp.JSON200 == nil {
		return copyBulkOverwriteResponse{}, apierror.FromResponse(resp.HTTPResponse, resp.Body)
	}
	return copyOverwriteResponse(*resp.JSON200), nil
}

func copyListResponse(response internalapi.ListRecordsResponse) copyListRecordsResponse {
	if response.Records == nil {
		return copyListRecordsResponse{}
	}
	records := make([]copyRecord, 0, len(*response.Records))
	for _, record := range *response.Records {
		records = append(records, copyRecordFromInternal(record))
	}
	return copyListRecordsResponse{Records: &records}
}

func copyRecordFromInternal(value internalapi.InternalRecord) copyRecord {
	return copyRecord{
		AccessMethods:    value.AccessMethods,
		ControlledAccess: value.ControlledAccess,
		CreatedTime:      value.CreatedTime,
		Description:      value.Description,
		Did:              value.Did,
		Name:             value.Name,
		Hashes:           copyHashInfoFromInternal(value.Hashes),
		Organization:     value.Organization,
		Project:          value.Project,
		Size:             value.Size,
		UpdatedTime:      value.UpdatedTime,
		Version:          value.Version,
	}
}

func copyRecordFromResponse(value internalapi.InternalRecordResponse) copyRecord {
	return copyRecord{
		AccessMethods:    value.AccessMethods,
		ControlledAccess: value.ControlledAccess,
		CreatedTime:      value.CreatedTime,
		Description:      value.Description,
		Did:              value.Did,
		Name:             value.Name,
		Hashes:           copyHashInfoFromInternal(value.Hashes),
		Organization:     value.Organization,
		Project:          value.Project,
		Size:             value.Size,
		UpdatedTime:      value.UpdatedTime,
		Version:          value.Version,
	}
}

func internalRecordFromCopyRecord(value copyRecord) internalapi.InternalRecord {
	return internalapi.InternalRecord{
		AccessMethods:    value.AccessMethods,
		ControlledAccess: value.ControlledAccess,
		CreatedTime:      value.CreatedTime,
		Description:      value.Description,
		Did:              value.Did,
		Name:             value.Name,
		Hashes:           internalHashInfoFromCopy(value.Hashes),
		Organization:     value.Organization,
		Project:          value.Project,
		Size:             value.Size,
		UpdatedTime:      value.UpdatedTime,
		Version:          value.Version,
	}
}

func copyHashInfoFromInternal(value *internalapi.HashInfo) *copyHashInfo {
	if value == nil {
		return nil
	}
	converted := copyHashInfo{}
	for key, value := range *value {
		converted[key] = value
	}
	return &converted
}

func internalHashInfoFromCopy(value *copyHashInfo) *internalapi.HashInfo {
	if value == nil {
		return nil
	}
	converted := internalapi.HashInfo{}
	for key, value := range *value {
		converted[key] = value
	}
	return &converted
}

func copyOverwriteResponse(value internalapi.BulkOverwriteResponse) copyBulkOverwriteResponse {
	return copyBulkOverwriteResponse{
		Processed:       value.Processed,
		Created:         value.Created,
		Replaced:        value.Replaced,
		DIDMatched:      value.DidMatched,
		ChecksumMatched: value.ChecksumMatched,
	}
}

func canonicalAccessMethod(method drsapi.AccessMethod) string {
	b, err := json.Marshal(method)
	if err != nil {
		return fmt.Sprintf("%s|%v", method.Type, method.AccessId)
	}
	return string(b)
}
