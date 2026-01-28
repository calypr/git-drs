package transferref

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/projectdir"
	"github.com/spf13/cobra"
)

var (
	drsClient client.DRSClient
	sConfig   sonic.API = sonic.ConfigFastest
)

// TODO: used for AnvIL use case, requires implementation
var Cmd = &cobra.Command{
	Use:   "transfer-ref",
	Short: "[RUN VIA GIT LFS] handle transfers of existing DRS object into git during git push",
	Long:  "[RUN VIA GIT LFS] custom transfer mechanism to pull LFS files during git lfs pull. Does nothing on push.",
	RunE: func(cmd *cobra.Command, args []string) error {
		//setup logging to file for debugging
		myLogger := drslog.GetLogger()

		myLogger.Info("~~~~~~~~~~~~~ START: custom anvil transfer ~~~~~~~~~~~~~")

		scanner := bufio.NewScanner(os.Stdin)
		encoder := encoder.NewStreamEncoder(os.Stdout)

		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Error(fmt.Sprintf("Error loading config: %v", err))
			return err
		}

		var remoteName string

		// Read the first (init) message outside the main loop
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			myLogger.Error(fmt.Sprintf("Error: %s", err))
			// No OID yet, so pass empty string
			lfs.WriteErrorMessage(encoder, "", 400, err.Error())
			return err
		}

		var initMsg map[string]any
		if err := sConfig.Unmarshal(scanner.Bytes(), &initMsg); err != nil {
			myLogger.Error(fmt.Sprintf("error decoding initial JSON message: %s", err))
			return err
		}

		// Handle "init" event and extract remote
		if evt, ok := initMsg["event"]; ok && evt == "init" {
			// if no remote name specified, use default remote
			defaultRemote, err := cfg.GetDefaultRemote()
			if err != nil {
				myLogger.Error(fmt.Sprintf("Error getting default remote: %v", err))
				lfs.WriteErrorMessage(encoder, "", 400, err.Error())
				return err
			}
			remoteName = string(defaultRemote)
			myLogger.Debug(fmt.Sprintf("Initializing connection, remote not specified — using default: %s", remoteName))

			// Respond with an empty json object via stdout
			encoder.Encode(struct{}{})
		} else {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%v'", initMsg["event"])
			myLogger.Error(fmt.Sprintf("Error: %s", err))
			lfs.WriteErrorMessage(encoder, "", 400, err.Error())
			return err
		}

		drsClient, err = cfg.GetRemoteClient(config.Remote(remoteName), myLogger)
		if err != nil {
			myLogger.Error(fmt.Sprintf("Error creating indexd client: %s", err))
			lfs.WriteErrorMessage(encoder, "", 400, err.Error())
			return err
		}

		for scanner.Scan() {
			var msg map[string]any
			err := sConfig.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				myLogger.Debug(fmt.Sprintf("error decoding JSON: %s", err))
				continue
			}
			myLogger.Debug(fmt.Sprintf("Received message: %s", msg))

			// Example: handle only "init" event
			if evt, ok := msg["event"]; ok && evt == "init" {
				// Log for debugging
				myLogger.Debug(fmt.Sprintf("Handling init: %s", msg))

				// Respond with an empty json object via stdout
				encoder.Encode(struct{}{})
				myLogger.Debug("Responding to init with empty object")
			} else if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				myLogger.Debug(fmt.Sprintf("Handling download event: %s", msg))

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := sConfig.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v\n", err)
					myLogger.Error(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 400, errMsg)
					continue
				}

				// call DRS Downloader via downloadFile
				dstPath, err := downloadFile(config.Remote(remoteName), downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v\n", downloadMsg.Oid, err)
					myLogger.Error(errMsg)
					lfs.WriteErrorMessage(encoder, downloadMsg.Oid, 500, errMsg)
					continue
				}

				myLogger.Debug(fmt.Sprintf("Downloaded file for OID %s", downloadMsg.Oid))

				// send success message back
				myLogger.Debug(fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				}
				encoder.Encode(completeMsg)
			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				myLogger.Info(fmt.Sprintf("Handling upload event: %s", msg))
				myLogger.Info("skipping upload, just registering existing DRS object")

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := sConfig.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v\n", err)
					myLogger.Error(errMsg)
					lfs.WriteErrorMessage(encoder, uploadMsg.Oid, 400, errMsg)
				}
				myLogger.Debug(fmt.Sprintf("Got UploadMessage: %+v", uploadMsg))

				// send success message back
				completeMsg := lfs.CompleteMessage{
					Event: "complete",
					Oid:   uploadMsg.Oid,
				}
				myLogger.Info(fmt.Sprintf("Complete message: %+v", completeMsg))
				encoder.Encode(completeMsg)
			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				// Handle terminate event
				myLogger.Debug(fmt.Sprintf("terminate event received: %s", msg))
			}
		}

		if err := scanner.Err(); err != nil {
			myLogger.Debug(fmt.Sprintf("stdin error: %s", err))
		}

		myLogger.Info("~~~~~~~~~~~~~ COMPLETED: custom anvil transfer ~~~~~~~~~~~~~")

		return nil
	},
}

func downloadFile(remote config.Remote, sha string) (string, error) {
	myLogger := drslog.GetLogger()

	myLogger.Debug(fmt.Sprintf("Downloading file for sha %s", sha))

	// get terra project
	cfg, err := config.LoadConfig() // should this be handled only via indexd client?
	if err != nil {
		return "", fmt.Errorf("error loading config: %v", err)
	}

	cli, err := cfg.GetRemoteClient(remote, myLogger)
	if err != nil {
		return "", err
	}

	terraProject := cli.GetProjectId()

	filePath, err := drsmap.GetObjectPath(projectdir.DRS_REF_DIR, sha)
	if err != nil {
		return "", fmt.Errorf("error getting object path for sha %s: %v", sha, err)
	}
	myLogger.Debug(fmt.Sprintf("File path for sha %s: %s", sha, filePath))

	// get DRS URI in the second line of the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer file.Close()
	myLogger.Debug(fmt.Sprintf("Opened file %s for reading", filePath))

	scanner := bufio.NewScanner(file)
	var drsUri string
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		myLogger.Debug(fmt.Sprintf("Reading line %d: %s", lineNum, line))
		if lineNum == 2 {
			// second line should be the DRS URI
			drsUri = strings.TrimSpace(line)
			myLogger.Debug(fmt.Sprintf("DRS URI found: %s", drsUri))
			break
		}
	}

	myLogger.Debug(fmt.Sprintf("DRS URI found: %s", drsUri))
	if drsUri == "" {
		return "", fmt.Errorf("error: file %s does not contain a valid DRS URI in the second line", filePath)
	}
	drsObj, err := drsClient.GetObject(context.Background(), drsUri)
	if err != nil {
		return "", fmt.Errorf("error fetching DRS object for URI %s: %v", drsUri, err)
	}
	if drsObj == nil {
		return "", fmt.Errorf("no DRS object found for URI %s", drsUri)
	}

	myLogger.Debug(fmt.Sprintf("DRS Object fetched: %+v", drsObj))

	// call DRS downloader as a binary, redirect output to log file
	logFile, err := os.OpenFile(projectdir.DRS_LOG_FILE, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("error opening log file: %v", err)
	}
	defer logFile.Close()

	//TODO: This should be done in the DRSClient code
	// download file, make sure its name is the sha
	dstPath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, sha)
	dstDir := filepath.Dir(dstPath)
	cmd := exec.Command("drs_downloader", "terra", "--user-project", terraProject, "--manifest-path", filePath, "--destination-dir", dstDir)

	// write command to log file
	logFile.WriteString(fmt.Sprintf("Running command: %s\n", cmd.String()))

	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmdOut, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error running drs_downloader for sha %s: %s", sha, cmdOut)
	}

	//rename file to sha
	tmpPath := filepath.Join(dstDir, drsObj.Name)
	err = os.Rename(tmpPath, dstPath)
	if err != nil {
		return "", fmt.Errorf("error renaming downloaded file from %s to %s: %v", tmpPath, dstPath, err)
	}

	return dstPath, nil
}
