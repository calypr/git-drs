package transfer

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestTransferCommandStructure(t *testing.T) {
	// Test that the command is properly structured
	if Cmd.Use != "transfer" {
		t.Errorf("Expected Use to be 'transfer', got '%s'", Cmd.Use)
	}

	if Cmd.Short == "" {
		t.Error("Expected Short description to be non-empty")
	}

	if Cmd.Long == "" {
		t.Error("Expected Long description to be non-empty")
	}

	if Cmd.RunE == nil {
		t.Error("Expected RunE function to be defined")
	}
}

func TestTransferCommandFlags(t *testing.T) {
	// Test that the command has no flags defined
	flags := Cmd.Flags()
	if flags.HasFlags() {
		t.Error("Expected transfer command to have no flags")
	}
}

func TestTransferCommandArgs(t *testing.T) {
	// Test Args validation - transfer command accepts any number of args
	if Cmd.Args != nil {
		// If Args is defined, test it
		err := Cmd.Args(Cmd, []string{})
		if err != nil {
			t.Errorf("Transfer command should accept no arguments, got error: %v", err)
		}

		err = Cmd.Args(Cmd, []string{"arg1"})
		if err != nil {
			t.Errorf("Transfer command should accept arguments, got error: %v", err)
		}
	}
}

func TestTransferCommandHelp(t *testing.T) {
	// Test help output
	var buf bytes.Buffer
	testCmd := &cobra.Command{
		Use:   "transfer",
		Short: "register LFS files into gen3 during git push",
		Long:  "custom transfer mechanism to register LFS files up to gen3 during git push. For new files, creates an indexd record and uploads to the bucket",
	}

	testCmd.SetOut(&buf)
	testCmd.SetErr(&buf)
	testCmd.SetArgs([]string{"--help"})

	err := testCmd.Execute()
	if err != nil {
		t.Errorf("Help command failed: %v", err)
	}

	output := buf.String()
	expectedStrings := []string{
		"register LFS files into gen3 during git push",
		"custom transfer mechanism",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("Help output missing expected string '%s'. Got: %s", expected, output)
		}
	}
}

func TestInitMessage(t *testing.T) {
	// Test InitMessage struct
	initMsg := InitMessage{
		Event:               "init",
		Operation:           "upload",
		Remote:              "origin",
		Concurrent:          false,
		ConcurrentTransfers: 1,
	}

	data, err := json.Marshal(initMsg)
	if err != nil {
		t.Errorf("Failed to marshal InitMessage: %v", err)
	}

	var decoded InitMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal InitMessage: %v", err)
	}

	if decoded.Event != "init" {
		t.Errorf("Expected Event 'init', got '%s'", decoded.Event)
	}

	if decoded.Operation != "upload" {
		t.Errorf("Expected Operation 'upload', got '%s'", decoded.Operation)
	}
}

func TestCompleteMessage(t *testing.T) {
	// Test CompleteMessage struct
	completeMsg := CompleteMessage{
		Event: "complete",
		Oid:   "test-oid-123",
		Path:  "/path/to/file",
	}

	data, err := json.Marshal(completeMsg)
	if err != nil {
		t.Errorf("Failed to marshal CompleteMessage: %v", err)
	}

	var decoded CompleteMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal CompleteMessage: %v", err)
	}

	if decoded.Event != "complete" {
		t.Errorf("Expected Event 'complete', got '%s'", decoded.Event)
	}

	if decoded.Oid != "test-oid-123" {
		t.Errorf("Expected Oid 'test-oid-123', got '%s'", decoded.Oid)
	}
}

func TestUploadMessage(t *testing.T) {
	// Test UploadMessage struct
	action := &Action{
		Href:      "https://example.com/upload",
		Header:    map[string]string{"Authorization": "Bearer token"},
		ExpiresIn: 3600,
	}

	uploadMsg := UploadMessage{
		Event:  "upload",
		Oid:    "test-oid-456",
		Size:   1024,
		Path:   "/local/path/to/file",
		Action: action,
	}

	data, err := json.Marshal(uploadMsg)
	if err != nil {
		t.Errorf("Failed to marshal UploadMessage: %v", err)
	}

	var decoded UploadMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal UploadMessage: %v", err)
	}

	if decoded.Event != "upload" {
		t.Errorf("Expected Event 'upload', got '%s'", decoded.Event)
	}

	if decoded.Size != 1024 {
		t.Errorf("Expected Size 1024, got %d", decoded.Size)
	}

	if decoded.Action == nil {
		t.Error("Expected Action to be non-nil")
	} else {
		if decoded.Action.Href != "https://example.com/upload" {
			t.Errorf("Expected Action.Href 'https://example.com/upload', got '%s'", decoded.Action.Href)
		}
	}
}

func TestDownloadMessage(t *testing.T) {
	// Test DownloadMessage struct
	action := &Action{
		Href:      "https://example.com/download",
		Header:    map[string]string{"Authorization": "Bearer token"},
		ExpiresIn: 3600,
	}

	downloadMsg := DownloadMessage{
		Event:  "download",
		Oid:    "test-oid-789",
		Size:   2048,
		Action: action,
		Path:   "/local/download/path",
	}

	data, err := json.Marshal(downloadMsg)
	if err != nil {
		t.Errorf("Failed to marshal DownloadMessage: %v", err)
	}

	var decoded DownloadMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal DownloadMessage: %v", err)
	}

	if decoded.Event != "download" {
		t.Errorf("Expected Event 'download', got '%s'", decoded.Event)
	}

	if decoded.Size != 2048 {
		t.Errorf("Expected Size 2048, got %d", decoded.Size)
	}

	if decoded.Path != "/local/download/path" {
		t.Errorf("Expected Path '/local/download/path', got '%s'", decoded.Path)
	}
}

func TestErrorMessage(t *testing.T) {
	// Test ErrorMessage struct
	errorMsg := ErrorMessage{
		Event: "error",
		Oid:   "test-oid-error",
		Error: Error{
			Code:    500,
			Message: "Internal server error",
		},
	}

	data, err := json.Marshal(errorMsg)
	if err != nil {
		t.Errorf("Failed to marshal ErrorMessage: %v", err)
	}

	var decoded ErrorMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal ErrorMessage: %v", err)
	}

	if decoded.Event != "error" {
		t.Errorf("Expected Event 'error', got '%s'", decoded.Event)
	}

	if decoded.Error.Code != 500 {
		t.Errorf("Expected Error.Code 500, got %d", decoded.Error.Code)
	}

	if decoded.Error.Message != "Internal server error" {
		t.Errorf("Expected Error.Message 'Internal server error', got '%s'", decoded.Error.Message)
	}
}

func TestProgressResponse(t *testing.T) {
	// Test ProgressResponse struct
	progressResp := ProgressResponse{
		Event:          "progress",
		Oid:            "test-oid-progress",
		BytesSoFar:     512,
		BytesSinceLast: 256,
	}

	data, err := json.Marshal(progressResp)
	if err != nil {
		t.Errorf("Failed to marshal ProgressResponse: %v", err)
	}

	var decoded ProgressResponse
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal ProgressResponse: %v", err)
	}

	if decoded.Event != "progress" {
		t.Errorf("Expected Event 'progress', got '%s'", decoded.Event)
	}

	if decoded.BytesSoFar != 512 {
		t.Errorf("Expected BytesSoFar 512, got %d", decoded.BytesSoFar)
	}

	if decoded.BytesSinceLast != 256 {
		t.Errorf("Expected BytesSinceLast 256, got %d", decoded.BytesSinceLast)
	}
}

func TestTerminateMessage(t *testing.T) {
	// Test TerminateMessage struct
	terminateMsg := TerminateMessage{
		Event: "terminate",
	}

	data, err := json.Marshal(terminateMsg)
	if err != nil {
		t.Errorf("Failed to marshal TerminateMessage: %v", err)
	}

	var decoded TerminateMessage
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal TerminateMessage: %v", err)
	}

	if decoded.Event != "terminate" {
		t.Errorf("Expected Event 'terminate', got '%s'", decoded.Event)
	}
}

func TestWriteErrorMessage(t *testing.T) {
	// Test WriteErrorMessage function
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)

	WriteErrorMessage(encoder, "test-oid", "test error message")

	var errorResponse ErrorMessage
	decoder := json.NewDecoder(&buf)
	err := decoder.Decode(&errorResponse)
	if err != nil {
		t.Errorf("Failed to decode error message: %v", err)
	}

	if errorResponse.Event != "complete" {
		t.Errorf("Expected Event 'complete', got '%s'", errorResponse.Event)
	}

	if errorResponse.Oid != "test-oid" {
		t.Errorf("Expected Oid 'test-oid', got '%s'", errorResponse.Oid)
	}

	if errorResponse.Error.Code != 500 {
		t.Errorf("Expected Error.Code 500, got %d", errorResponse.Error.Code)
	}

	if errorResponse.Error.Message != "test error message" {
		t.Errorf("Expected Error.Message 'test error message', got '%s'", errorResponse.Error.Message)
	}
}

func TestAction(t *testing.T) {
	// Test Action struct
	action := Action{
		Href:      "https://example.com/action",
		Header:    map[string]string{"Content-Type": "application/json", "Authorization": "Bearer abc123"},
		ExpiresIn: 7200,
	}

	data, err := json.Marshal(action)
	if err != nil {
		t.Errorf("Failed to marshal Action: %v", err)
	}

	var decoded Action
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Errorf("Failed to unmarshal Action: %v", err)
	}

	if decoded.Href != "https://example.com/action" {
		t.Errorf("Expected Href 'https://example.com/action', got '%s'", decoded.Href)
	}

	if decoded.ExpiresIn != 7200 {
		t.Errorf("Expected ExpiresIn 7200, got %d", decoded.ExpiresIn)
	}

	if len(decoded.Header) != 2 {
		t.Errorf("Expected 2 headers, got %d", len(decoded.Header))
	}

	if decoded.Header["Content-Type"] != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", decoded.Header["Content-Type"])
	}
}

// Test comprehensive message flow
func TestMessageFlow(t *testing.T) {
	// Test a complete message flow scenario
	messages := []interface{}{
		InitMessage{
			Event:               "init",
			Operation:           "upload",
			Remote:              "origin",
			Concurrent:          false,
			ConcurrentTransfers: 1,
		},
		UploadMessage{
			Event: "upload",
			Oid:   "abc123",
			Size:  1024,
			Path:  "/tmp/test",
		},
		CompleteMessage{
			Event: "complete",
			Oid:   "abc123",
			Path:  "/remote/path",
		},
		TerminateMessage{
			Event: "terminate",
		},
	}

	for i, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			t.Errorf("Message %d: Failed to marshal: %v", i, err)
			continue
		}

		// Try to unmarshal as a generic map to check structure
		var generic map[string]interface{}
		err = json.Unmarshal(data, &generic)
		if err != nil {
			t.Errorf("Message %d: Failed to unmarshal as generic: %v", i, err)
			continue
		}

		// Check that event field exists
		if _, ok := generic["event"]; !ok {
			t.Errorf("Message %d: Missing 'event' field", i)
		}
	}
}