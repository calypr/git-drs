package copyrecords

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	internalapi "github.com/calypr/syfon/apigen/internalapi"
)

func TestRawIndexAPIBulkHashesDecodesSyfonResultsMap(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/index/bulk/hashes" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request internalapi.BulkHashesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if len(request.Hashes) != 1 || request.Hashes[0] != "abc" {
			t.Errorf("unexpected hashes: %#v", request.Hashes)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"abc":[{"did":"record-1","hashes":{"sha256":"abc"}}]}}`))
	}))
	defer server.Close()

	client, err := internalapi.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("new internal API client: %v", err)
	}
	response, err := newRawIndexAPI(client).BulkHashes(context.Background(), []string{"abc"})
	if err != nil {
		t.Fatalf("BulkHashes returned error: %v", err)
	}
	records := response.Results["abc"]
	if len(records) != 1 || records[0].Did != "record-1" {
		t.Fatalf("unexpected records: %#v", records)
	}
	if records[0].Hashes == nil || (*records[0].Hashes)["sha256"] != "abc" {
		t.Fatalf("unexpected record hashes: %#v", records[0].Hashes)
	}
}
