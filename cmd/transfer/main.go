package transfer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
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

		drsClient, err = client.NewIndexDClient(myLogger)
		if err != nil {
			myLogger.Logf("Error creating indexd client: %s", err)
			WriteErrorMessage(encoder, "", err.Error())
			return err
		}

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

				// Respond with an empty json object via stdout
				encoder.Encode(struct{}{})
				myLogger.Log("Responding to init with empty object")
			} else if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Logf("Handling download event: %s", msg)

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				// get signed url
				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}
				myLogger.Log(fmt.Sprintf("Got signed URL for OID %s: %+v", downloadMsg.Oid, accessUrl))
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
				myLogger.Log(fmt.Sprintf("Handling upload event: %s", msg))

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}
				myLogger.Log(fmt.Sprintf("Got UploadMessage: %+v\n", uploadMsg))

				// handle the upload via drs client (indexd client)
				drsObj, err := drsClient.RegisterFile(uploadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintln("Error registering file: " + err.Error())
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}

				myLogger.Logf("creating response message with oid %s", uploadMsg.Oid)

				// send success message back
				completeMsg := lfs.CompleteMessage{
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
