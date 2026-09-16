package addurl

import (
	"net/http"
	"strings"
	"testing"

	syrequest "github.com/calypr/syfon/client/request"
)

func TestMapInspectError_UpgradeMessageForMissingRoute(t *testing.T) {
	err := mapInspectError("s3://bucket/key", &syrequest.ResponseError{
		Method: http.MethodPost,
		URL:    "https://example.test/data/inspect",
		Status: http.StatusNotFound,
		Body:   "Cannot POST /data/inspect",
	})
	if err == nil || !strings.Contains(err.Error(), "upgrade Syfon") {
		t.Fatalf("expected upgrade message, got %v", err)
	}
}

func TestMapInspectError_ActionableForbidden(t *testing.T) {
	err := mapInspectError("s3://bucket/key", &syrequest.ResponseError{
		Method: http.MethodPost,
		URL:    "https://example.test/data/inspect",
		Status: http.StatusForbidden,
		Body:   "provider denied access to s3://bucket/key",
	})
	if err == nil || !strings.Contains(err.Error(), "was denied") {
		t.Fatalf("expected denied message, got %v", err)
	}
}
