package copyrecords

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	internalapi "github.com/calypr/syfon/apigen/internalapi"
	syservices "github.com/calypr/syfon/client/services"
)

func TestRawIndexAPIListUsesSyfonIndexService(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/index" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"hash":         "abc",
			"url":          "https://objects.example/file",
			"organization": "org",
			"project":      "project",
			"limit":        "25",
			"start":        "cursor",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("query %s = %q, want %q", key, got, want)
			}
		}
		if query.Get("page") != "" {
			t.Errorf("page query should be omitted when start is set, got %q", query.Get("page"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"records":[{"did":"record-1","hashes":{"sha256":"abc"},"size":42}]}`))
	}))
	defer server.Close()

	client, err := internalapi.NewClientWithResponses(server.URL)
	if err != nil {
		t.Fatalf("new internal API client: %v", err)
	}
	response, err := newRawIndexAPI(client).List(context.Background(), syservices.ListRecordsOptions{
		Hash:         "abc",
		URL:          "https://objects.example/file",
		Organization: "org",
		ProjectID:    "project",
		Limit:        25,
		Start:        "cursor",
		Page:         7,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if response.Records == nil || len(*response.Records) != 1 {
		t.Fatalf("unexpected records: %#v", response.Records)
	}
	record := (*response.Records)[0]
	if record.Did != "record-1" || record.Size == nil || *record.Size != 42 {
		t.Fatalf("unexpected record conversion: %#v", record)
	}
	if record.Hashes == nil || (*record.Hashes)["sha256"] != "abc" {
		t.Fatalf("unexpected record hashes: %#v", record.Hashes)
	}
}

func TestRawIndexAPIListReturnsSyfonErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		status      int
		body        string
		contentType string
	}{
		{name: "non-200", status: http.StatusBadGateway, body: `{"error":"upstream failed"}`, contentType: "application/json"},
		{name: "malformed JSON", status: http.StatusOK, body: `{"records":`, contentType: "application/json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, err := internalapi.NewClientWithResponses(server.URL)
			if err != nil {
				t.Fatalf("new internal API client: %v", err)
			}
			_, err = newRawIndexAPI(client).List(context.Background(), syservices.ListRecordsOptions{})
			if err == nil {
				t.Fatalf("List returned nil error for %s", test.name)
			}
		})
	}
}

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
