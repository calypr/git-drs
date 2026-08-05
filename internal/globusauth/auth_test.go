package globusauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClientFromEnvRequiresToken(t *testing.T) {
	t.Setenv(TransferTokenEnv, "")
	if _, err := NewClientFromEnv(); err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestCheckUsesTransferAPI(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/tasksummary" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"username": "alice@example.org"})
	}))
	defer server.Close()

	identity, err := Check(context.Background(), &Client{BaseURL: server.URL, HTTPClient: server.Client(), Token: "token"})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if identity != "alice@example.org" {
		t.Fatalf("identity = %q", identity)
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestSubmitTransferUsesSubmissionIDAndPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/submission_id":
			_ = json.NewEncoder(w).Encode(map[string]string{"value": "submission-1"})
		case "/transfer":
			var payload struct {
				SubmissionID        string `json:"submission_id"`
				SourceEndpoint      string `json:"source_endpoint"`
				DestinationEndpoint string `json:"destination_endpoint"`
				Data                []struct {
					SourcePath      string `json:"source_path"`
					DestinationPath string `json:"destination_path"`
				} `json:"DATA"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if payload.SubmissionID != "submission-1" || payload.SourceEndpoint != "src" || payload.DestinationEndpoint != "dst" {
				t.Fatalf("unexpected payload: %+v", payload)
			}
			if len(payload.Data) != 1 || payload.Data[0].SourcePath != "/src/file" || payload.Data[0].DestinationPath != "/dst/file" {
				t.Fatalf("unexpected transfer item: %+v", payload.Data)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"task_id": "task-1"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	taskID, err := (&Client{BaseURL: server.URL, HTTPClient: server.Client(), Token: "token"}).SubmitTransfer(context.Background(), "src", "/src/file", "dst", "/dst/file", "label")
	if err != nil {
		t.Fatalf("SubmitTransfer returned error: %v", err)
	}
	if taskID != "task-1" {
		t.Fatalf("taskID = %q", taskID)
	}
}

func TestWaitForTaskPollsUntilSucceeded(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/task/task-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		calls++
		status := "ACTIVE"
		if calls == 2 {
			status = "SUCCEEDED"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
	}))
	defer server.Close()

	err := (&Client{BaseURL: server.URL, HTTPClient: server.Client(), Token: "token"}).WaitForTask(context.Background(), "task-1", time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForTask returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestResponseBodySuffix(t *testing.T) {
	if got := ResponseBodySuffix([]byte(" failure \n")); !strings.Contains(got, "failure") {
		t.Fatalf("suffix = %q", got)
	}
}
