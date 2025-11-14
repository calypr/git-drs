package transfer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

var (
	req       lfs.InitMessage
	drsClient client.ObjectStoreClient
	operation string // "upload" or "download", set by the init message
)
var Cmd = &cobra.Command{
	Use:   "transfer",
	Short: "[RUN VIA GIT LFS] register LFS files into gen3 during git push",
	Long:  "[RUN VIA GIT LFS] custom transfer mechanism to register LFS files up to gen3 during git push. For new files, creates an indexd record and uploads to the bucket",
	RunE: func(cmd *cobra.Command, args []string) error {
		//setup logging to file for debugging

		myLogger, err := client.NewLogger("", false)
		if err != nil {
			return err
		}

		defer myLogger.Close()
		myLogger.Log("~~~~~~~~~~~~~ START: custom transfer ~~~~~~~~~~~~~")

		scanner := bufio.NewScanner(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)

		var remoteName string

		// Read the first (init) message outside the main loop
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			myLogger.Logf("Error: %s", err)
			// No OID yet, so pass empty string
			lfs.WriteErrorMessage(encoder, "", err.Error())
			return err
		}

		var initMsg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &initMsg); err != nil {
			myLogger.Logf("error decoding initial JSON message: %s", err)
			return err
		}

		// Handle "init" event and extract remote
		if evt, ok := initMsg["event"]; ok && evt == "init" {
			if r, ok := initMsg["remote"].(string); ok {
				remoteName = r
				myLogger.Logf("Initializing connection. Remote used: %s", remoteName)
			} else {
				myLogger.Log("Initializing connection, but remote field was not found or wasn't a string.")
			}

			// Respond with an empty json object via stdout
			encoder.Encode(struct{}{})
		} else {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%v'", initMsg["event"])
			myLogger.Logf("Error: %s", err)
			lfs.WriteErrorMessage(encoder, "", err.Error())
			return err
		}

		// Pass the extracted remoteName to NewIndexDClient
		drsClient, err = client.NewIndexDClient(myLogger, config.Profile(remoteName))
		if err != nil {
			myLogger.Logf("Error creating indexd client: %s", err)
			lfs.WriteErrorMessage(encoder, "", err.Error())
			return err
		}

		// The scanner is now positioned for the next message (download/upload/terminate)
		for scanner.Scan() {
			var msg map[string]any
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				myLogger.Logf("error decoding JSON: %s", err)
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Logf("Download requested")

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}
				myLogger.Logf("Downloading file OID %s", downloadMsg.Oid)

				// get signed url
				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid, &downloadMsg.Path)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}
				if accessUrl.URL == "" {
					errMsg := fmt.Sprintf("Unable to get access URL %s", downloadMsg.Oid)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}

				// download signed url
				dstPath, err := client.GetObjectPath(config.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}
				err = client.DownloadSignedUrl(accessUrl.URL, dstPath)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}

				// send success message back
				myLogger.Log(fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				}
				encoder.Encode(completeMsg)

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				myLogger.Log(fmt.Sprintf("Upload requested"))

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}
				myLogger.Log(fmt.Sprintf("Uploading file OID %s", uploadMsg.Oid))

				// otherwise, register the file (create indexd record and upload file)
				drsObj, err := drsClient.RegisterFile(&uploadMsg)
				if err != nil {
					errMsg := fmt.Sprintln("Error registering file: " + err.Error())
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}

				// send success message back
				lfs.WriteCompleteMessage(encoder, uploadMsg.Oid, drsObj.Name)
				myLogger.Logf("Upload for OID %s complete", uploadMsg.Oid)

			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				// Handle terminate event
				myLogger.Log(fmt.Sprintf("LFS transfer complete"))
			}
		}

		if err := scanner.Err(); err != nil {
			myLogger.Log(fmt.Sprintf("stdin error: %s", err))
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return nil
	},
}
