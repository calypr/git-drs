package transferref

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/cmd/addref"
	"github.com/bmeg/git-drs/lfs"
	"github.com/bmeg/git-drs/utils"
	"github.com/spf13/cobra"
)

var (
	req       lfs.InitMessage
	drsClient client.ObjectStoreClient
	operation string // "upload" or "download", set by the init message
)

var Cmd = &cobra.Command{
	Use:   "transfer-ref",
	Short: "[RUN VIA GIT LFS] handle transfers of existing DRS object into git during git push",
	Long:  "[RUN VIA GIT LFS] custom transfer mechanism to pull LFS files during git lfs pull. Does nothing on push.",
	RunE: func(cmd *cobra.Command, args []string) error {
		//setup logging to file for debugging
		myLogger, err := client.NewLogger(utils.DRS_LOG_FILE)
		if err != nil {
			return fmt.Errorf("Failed to open log file: %v", err)
		}
		defer myLogger.Close()

		myLogger.Log("~~~~~~~~~~~~~ START: custom anvil transfer ~~~~~~~~~~~~~")

		scanner := bufio.NewScanner(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)

		for scanner.Scan() {
			var msg map[string]interface{}
			err := json.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				myLogger.Log(fmt.Sprintf("error decoding JSON: %s", err))
				continue
			}
			myLogger.Log(fmt.Sprintf("Received message: %s", msg))

			// Example: handle only "init" event
			if evt, ok := msg["event"]; ok && evt == "init" {
				// Log for debugging
				myLogger.Log(fmt.Sprintf("Handling init: %s", msg))

				// Respond with an empty json object via stdout
				encoder.Encode(struct{}{})
				myLogger.Log("Responding to init with empty object")
			} else if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Log(fmt.Sprintf("Handling download event: %s", msg))

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := json.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				// call DRS Downloader via downloadFile
				dstPath, err := downloadFile(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v\n", downloadMsg.Oid, err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, errMsg)
					continue
				}

				myLogger.Log(fmt.Sprintf("Downloaded file for OID %s", downloadMsg.Oid))

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
				myLogger.Log(fmt.Sprintf("skipping upload, just registering existing DRS object"))

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := json.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Log(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, errMsg)
				}
				myLogger.Log(fmt.Sprintf("Got UploadMessage: %+v\n", uploadMsg))

				// send success message back
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   uploadMsg.Oid,
				}
				myLogger.Log(fmt.Sprintf("Complete message: %+v", completeMsg))
				encoder.Encode(completeMsg)
			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				// Handle terminate event
				myLogger.Log(fmt.Sprintf("terminate event received: %s", msg))
			}
		}

		if err := scanner.Err(); err != nil {
			myLogger.Log(fmt.Sprintf("stdin error: %s", err))
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: custom anvil transfer ~~~~~~~~~~~~~")

		return nil
	},
}

func downloadFile(sha string) (string, error) {
	//setup logging to file for debugging
	myLogger, err := client.NewLogger(utils.DRS_LOG_FILE)
	if err != nil {
		return "", fmt.Errorf("Failed to open log file: %v", err)
	}
	defer myLogger.Close()
	myLogger.Log(fmt.Sprintf("Downloading file for sha %s", sha))

	// get terra project
	cfg, err := client.LoadConfig() // should this be handled only via indexd client?
	if err != nil {
		return "", fmt.Errorf("error loading config: %v", err)
	}

	// ensure we our current server is anvil
	if cfg.CurrentServer != client.ANVIL_TYPE {
		return "", fmt.Errorf("current server is not anvil, current server: %s. Please git drs init using --terra flag", cfg.CurrentServer)
	}

	terraProject := cfg.Servers.Anvil.Auth.TerraProject
	if terraProject == "" {
		return "", fmt.Errorf("error: project key is empty in config file")
	}

	filePath, err := client.GetObjectPath(utils.DRS_REF_DIR, sha)
	if err != nil {
		return "", fmt.Errorf("error getting object path for sha %s: %v", sha, err)
	}
	myLogger.Log(fmt.Sprintf("File path for sha %s: %s", sha, filePath))

	// get DRS URI in the second line of the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer file.Close()
	myLogger.Log(fmt.Sprintf("Opened file %s for reading", filePath))

	scanner := bufio.NewScanner(file)
	var drsUri string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		myLogger.Log(fmt.Sprintf("Reading line %d: %s", lineNum, line))
		if lineNum == 2 {
			// second line should be the DRS URI
			drsUri = strings.TrimSpace(line)
			myLogger.Log(fmt.Sprintf("DRS URI found: %s", drsUri))
			break
		}
	}

	myLogger.Log(fmt.Sprintf("DRS URI found: %s", drsUri))
	if drsUri == "" {
		return "", fmt.Errorf("error: file %s does not contain a valid DRS URI in the second line", filePath)
	}
	drsObj, err := addref.GetObject(drsUri)
	if err != nil {
		return "", fmt.Errorf("error fetching DRS object for URI %s: %v", drsUri, err)
	}
	if drsObj == nil {
		return "", fmt.Errorf("no DRS object found for URI %s", drsUri)
	}

	myLogger.Log(fmt.Sprintf("DRS Object fetched: %+v", drsObj))

	// call DRS downloader as a binary, redirect output to log file
	logFile, err := os.OpenFile(utils.DRS_LOG_FILE, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("error opening log file: %v", err)
	}
	defer logFile.Close()

	// download file, make sure its name is the sha
	dstPath, err := client.GetObjectPath(client.LFS_OBJS_PATH, sha)
	dstDir := filepath.Dir(dstPath)
	cmd := exec.Command("drs_downloader", "terra", "--user-project", terraProject, "--manifest-path", filePath, "--destination-dir", dstDir)

	// write command to log file
	logFile.WriteString(fmt.Sprintf("Running command: %s\n", cmd.String()))

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("error running drs_downloader for sha %s: %v", sha, err)
	}

	//rename file to sha
	tmpPath := filepath.Join(dstDir, drsObj.Name)
	err = os.Rename(tmpPath, dstPath)
	if err != nil {
		return "", fmt.Errorf("error renaming downloaded file from %s to %s: %v", tmpPath, dstPath, err)
	}

	return dstPath, nil
}
