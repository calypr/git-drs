package transfer

import (
	"bufio"
	"fmt"
	"os"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/spf13/cobra"
)

// TransferJob carries the raw JSON data and shared client
type TransferJob struct {
	data      []byte
	drsClient client.DRSClient
}

// TransferResult sent back to the single writer
type TransferResult struct {
	data    any
	isError bool
}

var (
	// Set once after init — determines which path all workers take
	transferOperation string    // "upload" or "download"
	sConfig           sonic.API = sonic.ConfigFastest
)

const (
	OPERATION_UPLOAD   = "upload"
	OPERATION_DOWNLOAD = "download"
)

var Cmd = &cobra.Command{
	Use:   "transfer",
	Short: "[RUN VIA GIT LFS] register LFS files into gen3 during git push",
	Long:  `[RUN VIA GIT LFS] git-lfs transfer mechanism to register LFS files up to gen3 during git push. For new files, creates an indexd record and uploads to the bucket`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()
		logger.Print("~~~~~~~~~~~~~ START: drs transfer ~~~~~~~~~~~~~")

		// Gotta go fast — big buffer
		scanner := bufio.NewScanner(os.Stdin)
		const maxCapacity = 10 * 1024 * 1024 // 10 MB
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)
		streamEncoder := encoder.NewStreamEncoder(os.Stdout)

		// Read init message
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			logger.Printf("Error: %v", err)
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		initBytes := make([]byte, len(scanner.Bytes()))
		copy(initBytes, scanner.Bytes())
		var initMsg lfs.InitMessage
		if err := sConfig.Unmarshal(initBytes, &initMsg); err != nil {
			logger.Printf("Error decoding initial JSON message: %v", err)
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		if initMsg.Event != "init" {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%s'", initMsg.Event)
			logger.Printf("Error: %v", err)
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		var drsClient client.DRSClient

		// Load config first
		cfg, err := config.LoadConfig()
		if err != nil {
			logger.Printf("Error loading config: %v", err)
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		// Determine remote
		remote, err := cfg.GetDefaultRemote()
		if err != nil {
			logger.Printf("Error getting default remote: %v", err)
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		drsClient, err = cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Printf("Error creating DRS client: %v", err)
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		// Determine if upload or download
		if initMsg.Operation == OPERATION_UPLOAD || initMsg.Operation == OPERATION_DOWNLOAD {
			transferOperation = initMsg.Operation
			logger.Printf("Transfer operation: %s", transferOperation)
		} else {
			err := fmt.Errorf("invalid or missing operation in init message: %s", initMsg.Operation)
			logger.Print(err.Error())
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}
		if err := streamEncoder.Encode(map[string]any{}); err != nil {
			logger.Printf("Error sending init acknowledgment: %v", err)
			return err
		}

		for scanner.Scan() {
			var msg map[string]any
			err := sConfig.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				logger.Printf("error decoding JSON: %s", err)
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				logger.Printf("Download requested")

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := sConfig.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v", err)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, "", 400, errMsg)
					continue
				}
				logger.Printf("Downloading file OID %s", downloadMsg.Oid)

				// get signed url
				accessUrl, err := drsClient.GetDownloadURL(downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting signed url for OID %s: %v", downloadMsg.Oid, err)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
					continue
				}
				if accessUrl.URL == "" {
					errMsg := fmt.Sprintf("Unable to get access URL for OID %s", downloadMsg.Oid)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 400, errMsg)
					continue
				}

				// download signed url
				dstPath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 400, errMsg)
					continue
				}
				err = s3_utils.DownloadSignedUrl(accessUrl.URL, dstPath)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", downloadMsg.Oid, err)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
					continue
				}

				// send success message back
				logger.Printf("Download for OID %s complete", downloadMsg.Oid)

				streamEncoder.Encode(lfs.CompleteMessage{
					Event: "complete",
					Oid:   downloadMsg.Oid,
					Path:  dstPath,
				})

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				logger.Printf("Upload requested")

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := sConfig.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v", err)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, uploadMsg.Oid, 400, errMsg)
					continue
				}
				logger.Printf("Uploading file OID %s", uploadMsg.Oid)
				drsObj, err := drsClient.RegisterFile(uploadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error registering file: %v\n", err)
					logger.Print(errMsg)
					lfs.WriteErrorMessage(streamEncoder, uploadMsg.Oid, 502, errMsg)
					continue
				}
				// send success message back
				lfs.WriteCompleteMessage(streamEncoder, uploadMsg.Oid, drsObj.Name)
				logger.Printf("Upload for OID %s complete", uploadMsg.Oid)

			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				logger.Printf("LFS transfer complete")
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Printf("stdin error: %s", err)
		}

		logger.Print("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return nil

	},
}
