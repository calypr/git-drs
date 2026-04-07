package drs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	gitclient "github.com/calypr/git-drs/client"
	"github.com/calypr/syfon/client/conf"
	datadrs "github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/mocks"
	"github.com/calypr/syfon/client/pkg/hash"
	"github.com/calypr/syfon/client/pkg/logs"
	"github.com/calypr/syfon/client/pkg/request"
	"go.uber.org/mock/gomock"
)

func newTestCtxWithEndpoint(t *testing.T, endpoint string) *gitclient.GitContext {
	t.Helper()
	ctx, err := newContextFromEndpoint(endpoint, "org", "proj", "bucket")
	if err != nil {
		t.Fatalf("newContextFromEndpoint(%s): %v", endpoint, err)
	}
	return ctx
}

// newContextFromEndpoint creates a lightweight GitContext for tests by wiring
// the Gen3 DRS client directly from a raw endpoint URL.
func newContextFromEndpoint(endpoint, org, project, bucket string) (*gitclient.GitContext, error) {
	baseLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dLogger := logs.NewGen3Logger(baseLogger, "", "test")
	cred := &conf.Credential{
		Profile:     "test",
		APIEndpoint: endpoint,
		AccessToken: "test-token",
	}
	req := request.NewRequestInterface(dLogger, cred, conf.NewConfigure(baseLogger))
	api := datadrs.NewDrsClient(req, cred, dLogger).
		WithOrganization(org).
		WithProject(project).
		WithBucket(bucket)

	return &gitclient.GitContext{
		API:          api,
		Organization: org,
		ProjectId:    project,
		BucketName:   bucket,
		Logger:       baseLogger,
		Credential:   cred,
	}, nil
}

func TestGetDownloadURLFromRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ga4gh/drs/v1/objects/") && strings.Contains(r.URL.Path, "/access/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"url":"https://download.example/file"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cl := newTestCtxWithEndpoint(t, srv.URL)

	t.Run("no records", func(t *testing.T) {
		_, err := ResolveGitScopedURL(context.Background(), cl.API, "oid1", cl.Organization, cl.ProjectId, cl.Logger)
		if err == nil {
			t.Fatal("expected error for no records")
		}
	})
}

func TestGetObjectByHashForGit_FiltersByProjectAuthz(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := mocks.NewMockDrsClient(ctrl)
	mockAPI.EXPECT().
		GetObjectByHash(gomock.Any(), &hash.Checksum{Type: string(hash.ChecksumTypeSHA256), Checksum: "oid1"}).
		Return([]datadrs.DRSObject{
			{
				Id: "did-match",
				AccessMethods: []datadrs.AccessMethod{{
					Type: "s3",
					Authorizations: datadrs.Authorizations{
						BearerAuthIssuers: []string{"/programs/org/projects/proj"},
					},
				}},
			},
			{
				Id: "did-other",
				AccessMethods: []datadrs.AccessMethod{{
					Type: "s3",
					Authorizations: datadrs.Authorizations{
						BearerAuthIssuers: []string{"/programs/other/projects/proj"},
					},
				}},
			},
		}, nil)

	gitCtx := &gitclient.GitContext{
		API:          mockAPI,
		Organization: "org",
		ProjectId:    "proj",
	}

	res, err := GetObjectByHashForGit(context.Background(), gitCtx.API, "oid1", gitCtx.Organization, gitCtx.ProjectId)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Id != "did-match" {
		t.Fatalf("unexpected filtered records: %#v", res)
	}
}

func TestDeleteRecordsByOID_DeletesAllMatchingDIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := mocks.NewMockDrsClient(ctrl)
	mockAPI.EXPECT().
		GetObjectByHash(gomock.Any(), gomock.Any()).
		Return([]datadrs.DRSObject{
			{Id: "did-a"},
			{Id: "did-b"},
		}, nil)
	mockAPI.EXPECT().DeleteRecord(gomock.Any(), "did-a").Return(nil)
	mockAPI.EXPECT().DeleteRecord(gomock.Any(), "did-b").Return(nil)

	if err := DeleteRecordsByOID(context.Background(), mockAPI, "oid1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteRecordsByOID_NoRecords(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAPI := mocks.NewMockDrsClient(ctrl)
	mockAPI.EXPECT().
		GetObjectByHash(gomock.Any(), gomock.Any()).
		Return([]datadrs.DRSObject{}, nil)

	err := DeleteRecordsByOID(context.Background(), mockAPI, "oid1")
	if err == nil {
		t.Fatal("expected error for no records")
	}
}
