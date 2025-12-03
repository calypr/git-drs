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
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		// ensure any panic is converted to an error return and reported
		var myLogger *client.Logger
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("panic recovered: %v", r)
				if myLogger != nil {
					myLogger.Log(msg)
				} else {
					fmt.Fprintln(os.Stderr, msg)
				}
				// try to write LFS error message to stdout
				encoder := json.NewEncoder(os.Stdout)
				lfs.WriteErrorMessage(encoder, "", msg)
				if retErr == nil {
					retErr = fmt.Errorf("panic: %v", r)
				}
			}
		}()

		// setup scanner/encoder early so defer can write to stdout on panic
		scanner := bufio.NewScanner(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)

		// setup logging to file for debugging
		var err error
		myLogger, err = client.NewLogger("", false)
		if err != nil {
			// critical: cannot continue without logger
			lfs.WriteErrorMessage(encoder, "", err.Error())
			retErr = err
			return
		}
		defer myLogger.Close()
		myLogger.Log("~~~~~~~~~~~~~ START: custom transfer ~~~~~~~~~~~~~")

		drsClient, err = client.NewIndexDClient(myLogger)
		if err != nil {
			myLogger.Logf("Error creating indexd client: %s", err)
			lfs.WriteErrorMessage(encoder, "", err.Error())
			retErr = err
			return
		}

		for scanner.Scan() {
			var msg map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
				myLogger.Logf("error decoding JSON: %s", err)
				// continue reading other messages; not fatal
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "init" {
				encoder.Encode(struct{}{})
				pid := os.Getpid()
				myLogger.Log(fmt.Sprintf("Initializing connection [git-drs pid=%d] ", pid))
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "download" {
				myLogger.Logf("Download requested")

				var downloadMsg lfs.DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					// treat as non-fatal for the transfer loop (continue reading)
					continue
				}
				if accessUrl.URL == "" {
					errMsg := fmt.Sprintf("Unable to get access URL %s", downloadMsg.Oid)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				dstPath, err := client.GetObjectPath(config.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}
				if err = client.DownloadSignedUrl(accessUrl.URL, dstPath); err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				myLogger.Log(fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				}
				encoder.Encode(completeMsg)
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "upload" {
				myLogger.Log(fmt.Sprintf("Upload requested"))

				var uploadMsg lfs.UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
					continue
				}

				drsObj, err := drsClient.RegisterFile(uploadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintln("Error registering file: " + err.Error())
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
					continue
				}

				lfs.WriteCompleteMessage(encoder, uploadMsg.Oid, drsObj.Name)
				myLogger.Logf("Upload for OID %s complete", uploadMsg.Oid)
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "terminate" {
				myLogger.Log(fmt.Sprintf("LFS transfer complete"))
			}
		}

		if serr := scanner.Err(); serr != nil {
			myLogger.Log(fmt.Sprintf("stdin error: %s", serr))
			retErr = serr
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return
	},
}
