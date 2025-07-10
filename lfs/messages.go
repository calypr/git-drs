package lfs

import "encoding/json"

// InitMessage represents the structure of the initiation data
type InitMessage struct {
	Event               string `json:"event"`               // Always "init" to identify this message
	Operation           string `json:"operation"`           // "upload" or "download" depending on transfer direction
	Remote              string `json:"remote"`              // Git remote name or URL
	Concurrent          bool   `json:"concurrent"`          // Reflects lfs.customtransfer.<name>.concurrent
	ConcurrentTransfers int    `json:"concurrenttransfers"` // Reflects lfs.concurrenttransfers value
}

// CompleteMessage is a minimal response to signal transfer is "complete"
type CompleteMessage struct {
	Event string `json:"event"`
	Oid   string `json:"oid"`
	Path  string `json:"path,omitempty"`
}

// UploadMessage represents a request to upload an object.
type UploadMessage struct {
	Event  string  `json:"event"`  // "upload"
	Oid    string  `json:"oid"`    // Object ID (SHA-256 hash)
	Size   int64   `json:"size"`   // Size in bytes
	Path   string  `json:"path"`   // Local path to file
	Action *Action `json:"action"` // Transfer action details (optional, may be omitted)
}

// DownloadMessage represents a request to download an object.
type DownloadMessage struct {
	Event  string  `json:"event"`  // "download"
	Oid    string  `json:"oid"`    // Object ID (SHA-256 hash)
	Size   int64   `json:"size"`   // Size in bytes
	Action *Action `json:"action"` // Transfer action details (optional, may be omitted)
	Path   string  `json:"path"`   // Where to store the downloaded file
}

// TerminateMessage is sent when the agent should terminate.
type TerminateMessage struct {
	Event string `json:"event"` // "terminate"
}

// ErrorMessage is sent when an error occurs during a transfer.
type ErrorMessage struct {
	Event string `json:"event"` // "error"
	Oid   string `json:"oid"`   // Object ID involved in the error
	Error Error  `json:"error"` // Error details
}

type Error struct {
	Code    int    `json:"code"`    // Error code (standard or custom)
	Message string `json:"message"` // Human-readable error message
}

// ProgressResponse provides progress updates for an object transfer.
type ProgressResponse struct {
	Event          string `json:"event"`          // "progress"
	Oid            string `json:"oid"`            // Object ID being transferred
	BytesSoFar     int64  `json:"bytesSoFar"`     // Bytes transferred so far
	BytesSinceLast int64  `json:"bytesSinceLast"` // Bytes transferred since last progress message
}

// TerminateResponse signals the agent has completed termination.
type TerminateResponse struct {
	Event string `json:"event"` // "terminate"
}

// Action is an optional struct representing transfer actions (upload/download URLs, etc.)
type Action struct {
	Href      string            `json:"href"`
	Header    map[string]string `json:"header,omitempty"`
	ExpiresIn int               `json:"expires_in,omitempty"`
}

func WriteErrorMessage(encoder *json.Encoder, oid string, errMsg string) {
	// create failure message and send it back
	errorResponse := ErrorMessage{
		Event: "complete",
		Oid:   oid,
		Error: Error{
			Code:    500,
			Message: errMsg,
		},
	}
	encoder.Encode(errorResponse)
}
