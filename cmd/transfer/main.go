package transfer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/bmeg/git-drs/client"
	"github.com/spf13/cobra"
)

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
	Path  string `json:"path"`
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

var (
	req       InitMessage
	drsClient client.ObjectStoreClient
	operation string // "upload" or "download", set by the init message
)

var Cmd = &cobra.Command{
	Use:   "transfer",
	Short: "register LFS files into gen3 during git push",
	Long:  "custom transfer mechanism to register LFS files up to gen3 during git push. For new files, creates an indexd record and uploads to the bucket",
	RunE: func(cmd *cobra.Command, args []string) error {
		//setup logging to file for debugging
		myLogger, err := client.NewLogger("")
		if err != nil {
			log.Printf("Failed to open log file: %v", err)
			return err
		}
		defer myLogger.Close()

		myLogger.Log("~~~~~~~~~~~~~ START: custom transfer ~~~~~~~~~~~~~")

		scanner := bufio.NewScanner(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)

		for scanner.Scan() {
			var msg map[string]any
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				myLogger.Logf("error decoding JSON: %s", err)
				continue
			}
			myLogger.Logf("Received message: %s", msg)

			// Example: handle only "init" event
			if evt, ok := msg["event"]; ok && evt == "init" {

				drsClient, err = client.NewIndexDClient()
				if err != nil {
					myLogger.Logf("Error creating indexd client: %s", err)
					continue
				}

				// Respond with an empty json object via stdout
				encoder.Encode(struct{}{})
				myLogger.Log("Responding to init with empty object")
			} else if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Logf("Handling download event: %s", msg)

				// get download message
				var downloadMsg DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				// get signed url
				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}
				myLogger.Log(fmt.Sprintf("Got signed URL for OID %s: %+v", downloadMsg.Oid, accessUrl))
				if accessUrl.URL == "" {
					errMsg := fmt.Sprintf("Unable to get access URL %s", downloadMsg.Oid)
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}

				// download signed url
				dstPath, err := client.GetObjectPath(client.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}
				err = client.DownloadSignedUrl(accessUrl.URL, dstPath)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}

				// send success message back
				myLogger.Log(fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))
				completeMsg := CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				}
				encoder.Encode(completeMsg)

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				myLogger.Log(fmt.Sprintf("Handling upload event: %s", msg))

				// create UploadMessage from the received message
				var uploadMsg UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}
				myLogger.Log(fmt.Sprintf("Got UploadMessage: %+v\n", uploadMsg))

				// handle the upload via drs client (indexd client)
				drsObj, err := drsClient.RegisterFile(uploadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintln("Error registering file: " + err.Error())
					myLogger.Log(errMsg)
					WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}

				myLogger.Logf("creating response message with oid %s", uploadMsg.Oid)

				// send success message back
				completeMsg := CompleteMessage{
					Event: "complete",
					Oid:   uploadMsg.Oid,
					Path:  drsObj.Name,
				}
				myLogger.Log(fmt.Sprintf("Complete message: %+v", completeMsg))
				encoder.Encode(completeMsg)

				myLogger.Logf("Upload for oid %s complete", uploadMsg.Oid)
			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				// Handle terminate event
				myLogger.Log(fmt.Sprintf("terminate event received: %s", msg))
			}
		}

		if err := scanner.Err(); err != nil {
			myLogger.Log(fmt.Sprintf("stdin error: %s", err))
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")

		return nil
	},
}

func WriteErrorMessage(encoder *json.Encoder, oid string, errMsg string) {
	// create failure message and send it back
	errorResponse := ErrorMessage{
		Event: "complete",
		Oid:   oid,
		Error: Error{
			Code:    1,
			Message: errMsg,
		},
	}
	encoder.Encode(errorResponse)
}
