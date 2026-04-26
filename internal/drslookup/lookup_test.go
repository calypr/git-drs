package drslookup

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syrequest "github.com/calypr/syfon/client/request"
)

type fakeRequester struct {
	method string
	path   string
	body   any
	resp   any
}

func (f *fakeRequester) Do(ctx context.Context, method, path string, body, out any, opts ...syrequest.RequestOption) error {
	f.method = method
	f.path = path
	f.body = body
	data, err := json.Marshal(f.resp)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func TestBulkAccessURLsForObjects(t *testing.T) {
	accessID := "s3"
	methods := []drsapi.AccessMethod{{
		Type:     drsapi.AccessMethodTypeS3,
		AccessId: &accessID,
	}}
	req := &fakeRequester{
		resp: map[string]any{
			"resolved_drs_object_access_urls": []map[string]any{
				{
					"drs_object_id": "obj-1",
					"drs_access_id": "s3",
					"url":           "https://signed.example/obj-1",
				},
			},
		},
	}
	got, err := BulkAccessURLsForObjects(context.Background(), &config.GitContext{Requestor: req}, []drsapi.DrsObject{
		{Id: "obj-1", AccessMethods: &methods},
	})
	if err != nil {
		t.Fatalf("BulkAccessURLsForObjects returned error: %v", err)
	}
	if req.method != http.MethodPost {
		t.Fatalf("expected POST, got %s", req.method)
	}
	if req.path != "/ga4gh/drs/v1/objects/access" {
		t.Fatalf("unexpected path: %s", req.path)
	}
	if got["obj-1"].Url != "https://signed.example/obj-1" {
		t.Fatalf("unexpected resolved URL: %+v", got)
	}
}
