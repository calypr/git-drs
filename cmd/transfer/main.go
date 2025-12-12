package transfer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/spf13/cobra"
)

var (
	req       lfs.InitMessage
	drsClient client.DRSClient
	operation string
)

var Cmd = &cobra.Command{
	Use:   "transfer",
	Short: "[RUN VIA GIT LFS] register LFS files into gen3 during git push",
	Long:  "[RUN VIA GIT LFS] git-lfs transfer mechanism to register LFS files up to gen3 during git push. For new files, creates an indexd record and uploads to the bucket",
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()

		myLogger.Print("~~~~~~~~~~~~~ START: drs transfer ~~~~~~~~~~~~~")

		scanner := bufio.NewScanner(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)

		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Printf("Error loading config: %v", err)
			return err
		}

		var remoteName string

		// Read the first (init) message outside the main loop
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			myLogger.Printf("Error: %s", err)
			// No OID yet, so pass empty string
			lfs.WriteErrorMessage(encoder, "", 400, err.Error())
			return err
		}

		var initMsg lfs.InitMessage
		if err := json.Unmarshal(scanner.Bytes(), &initMsg); err != nil {
			myLogger.Printf("error decoding initial JSON message: %s", err)
			return err
		}

		// Handle "init" event and extract remote
		if initMsg.Event == "init" {
			if initMsg.Remote != "" {
				remoteName = initMsg.Remote
				myLogger.Printf("Initializing connection. Remote used: %s", remoteName)
			} else {
				// If no remote name specified used origin
				remoteName = config.ORIGIN
				myLogger.Printf("Initializing connection, but remote field was not found or wasn't a string.")
			}

			remote := config.Remote(remoteName)
			drsClient, err = cfg.GetRemoteClient(remote, myLogger)
			if err != nil {
				myLogger.Printf("Error creating indexd client: %s", err)
				lfs.WriteInitErrorMessage(encoder, 400, err.Error())
				return err
			}

			// if upload event, prepare DRS objects
			if initMsg.Operation == "upload" {
				myLogger.Printf("Preparing DRS map for upload operation")
				err := drsmap.UpdateDrsObjects(drsClient, myLogger)
				if err != nil {
					myLogger.Printf("Error updating DRS map: %s", err)
					lfs.WriteInitErrorMessage(encoder, 400, err.Error())
					return err
				}
			}

			// Respond with an empty json object via stdout
			encoder.Encode(struct{}{})
		} else {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%s'", initMsg.Event)
			myLogger.Printf("Error: %s", err)
			lfs.WriteErrorMessage(encoder, "", 400, err.Error())
			return err
		}

		for scanner.Scan() {
			var msg map[string]any
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				myLogger.Printf("error decoding JSON: %s", err)
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Printf("Download requested")

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 400, errMsg)
					continue
				}
				myLogger.Printf("Downloading file OID %s", downloadMsg.Oid)

				// get signed url
				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 502, errMsg)
				}
				if accessUrl.URL == "" {
					errMsg := fmt.Sprintf("Unable to get access URL %s", downloadMsg.Oid)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 500, errMsg)
				}

				// download signed url
				dstPath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 400, errMsg)
					continue
				}
				err = s3_utils.DownloadSignedUrl(accessUrl.URL, dstPath)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 502, errMsg)
				}

				// send success message back
				myLogger.Printf("Download for OID %s complete", downloadMsg.Oid)
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				}
				encoder.Encode(completeMsg)

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				myLogger.Printf("Upload requested")

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, 400, errMsg)
				}
				myLogger.Printf("Uploading file OID %s", uploadMsg.Oid)

				// register file (write drs record and upload file)
				drsObj, err := drsClient.RegisterFile(uploadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintln("Error registering file: " + err.Error())
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, 502, errMsg)
				}
				// send success message back
				lfs.WriteCompleteMessage(encoder, uploadMsg.Oid, drsObj.Name)
				myLogger.Printf("Upload for OID %s complete", uploadMsg.Oid)

			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				// Handle terminate event
				myLogger.Printf("LFS transfer complete")
			}
		}

		if err := scanner.Err(); err != nil {
			myLogger.Printf("stdin error: %s", err)
		}

		myLogger.Print("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return nil
	},
}
