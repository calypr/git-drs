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
	operation string // "upload" or "download", set by the init message
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

		drsClient, err = cfg.GetCurrentRemoteClient(myLogger)
		if err != nil {
			myLogger.Printf("Error creating indexd client: %s", err)
			lfs.WriteErrorMessage(encoder, "", err.Error())
			return err
		}

		for scanner.Scan() {
			var msg map[string]any
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				myLogger.Printf("error decoding JSON: %s", err)
				continue
			}

			// Example: handle only "init" event
			if evt, ok := msg["event"]; ok && evt == "init" {

				// Respond with an empty json object via stdout
				encoder.Encode(struct{}{})
				myLogger.Print("Initializing connection")

			} else if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Printf("Download requested")

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}
				myLogger.Printf("Downloading file OID %s", downloadMsg.Oid)

				// get signed url
				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}
				if accessUrl.URL == "" {
					errMsg := fmt.Sprintf("Unable to get access URL %s", downloadMsg.Oid)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}

				// download signed url
				dstPath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}
				err = s3_utils.DownloadSignedUrl(accessUrl.URL, dstPath)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
				}

				// send success message back
				myLogger.Print(fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				}
				encoder.Encode(completeMsg)

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				myLogger.Print(fmt.Sprintf("Upload requested"))

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Print(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}
				myLogger.Print(fmt.Sprintf("Uploading file OID %s", uploadMsg.Oid))

				//TODO: write code to take Oid and generate DRSRecord
				// otherwise, register the file (create indexd record and upload file)
				myLogger.Print("Uploading files is not yet implemented")
				//TODO: re-implement this with new DRSClient methods
				/*
					drsObj, err := drsClient.RegisterFile(uploadMsg.Oid)
					if err != nil {
						errMsg := fmt.Sprintln("Error registering file: " + err.Error())
						myLogger.Print(errMsg)
						lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
					}
					// send success message back
					lfs.WriteCompleteMessage(encoder, uploadMsg.Oid, drsObj.Name)
					myLogger.Printf("Upload for OID %s complete", uploadMsg.Oid)
				*/

			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				// Handle terminate event
				myLogger.Print(fmt.Sprintf("LFS transfer complete"))
			}
		}

		if err := scanner.Err(); err != nil {
			myLogger.Print(fmt.Sprintf("stdin error: %s", err))
		}

		myLogger.Print("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return nil
	},
}
