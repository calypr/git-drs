package transfer

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	dataClientCommon "github.com/calypr/data-client/common"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/common"

	"github.com/calypr/data-client/hash"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
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
		logger.Info("~~~~~~~~~~~~~ START: drs transfer ~~~~~~~~~~~~~")

		// Gotta go fast — big buffer
		scanner := bufio.NewScanner(os.Stdin)
		const maxCapacity = 10 * 1024 * 1024 // 10 MB
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)
		streamEncoder := encoder.NewStreamEncoder(os.Stdout)

		// Read init message
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			logger.Error(fmt.Sprintf("Error: %v", err))
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		initBytes := make([]byte, len(scanner.Bytes()))
		copy(initBytes, scanner.Bytes())
		var initMsg lfs.InitMessage
		if err := sConfig.Unmarshal(initBytes, &initMsg); err != nil {
			logger.Error(fmt.Sprintf("Error decoding initial JSON message: %v", err))
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		if initMsg.Event != "init" {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%s'", initMsg.Event)
			logger.Error(fmt.Sprintf("Error: %v", err))
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		var drsClient client.DRSClient

		// Load config first
		cfg, err := config.LoadConfig()
		if err != nil {
			logger.Error(fmt.Sprintf("Error loading config: %v", err))
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		// Determine remote
		remote, err := cfg.GetDefaultRemote()
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting default remote: %v", err))
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		drsClient, err = cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Error(fmt.Sprintf("Error creating DRS client: %v", err))
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}

		// Determine if upload or download
		if initMsg.Operation == OPERATION_UPLOAD || initMsg.Operation == OPERATION_DOWNLOAD {
			transferOperation = initMsg.Operation
			logger.Debug(fmt.Sprintf("Transfer operation: %s", transferOperation))
		} else {
			err := fmt.Errorf("invalid or missing operation in init message: %s", initMsg.Operation)
			logger.Error(err.Error())
			lfs.WriteInitErrorMessage(streamEncoder, 400, err.Error())
			return err
		}
		if err := streamEncoder.Encode(map[string]any{}); err != nil {
			logger.Error(fmt.Sprintf("Error sending init acknowledgment: %v", err))
			return err
		}

		for scanner.Scan() {
			var msg map[string]any
			err := sConfig.Unmarshal(scanner.Bytes(), &msg)
			if err != nil {
				logger.Error(fmt.Sprintf("error decoding JSON: %s", err))
				continue
			}

			if evt, ok := msg["event"]; ok && evt == "download" {
				// Handle download event
				logger.Debug("Download requested")

				// get download message
				var downloadMsg lfs.DownloadMessage
				if err := sConfig.Unmarshal(scanner.Bytes(), &downloadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing downloadMessage: %v", err)
					logger.Error(errMsg)
					lfs.WriteErrorMessage(streamEncoder, "", 400, errMsg)
					continue
				}
				ctx := dataClientCommon.WithProgress(context.Background(), lfs.NewProgressCallback(streamEncoder))
				ctx = dataClientCommon.WithOid(ctx, downloadMsg.Oid)
				logger.InfoContext(ctx, fmt.Sprintf("Downloading file OID %s", downloadMsg.Oid))

				// get the matching record for this OID
				checksumSpec := &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: downloadMsg.Oid}
				records, err := drsClient.GetObjectByHash(ctx, checksumSpec)
				if err != nil {
					errMsg := fmt.Sprintf("Error looking up OID %s: %v", downloadMsg.Oid, err)
					logger.ErrorContext(ctx, errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
					continue
				}

				var matchingRecord *drs.DRSObject
				matchingRecord, err = drsmap.FindMatchingRecord(records, drsClient.GetOrganization(), drsClient.GetProjectId())
				if err != nil {
					errMsg := fmt.Sprintf("Error finding matching record for project %s: %v", drsClient.GetProjectId(), err)
					logger.ErrorContext(ctx, errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
					errMsg = fmt.Sprintf("Error getting signed URL for OID %s: %v", downloadMsg.Oid, err)
					logger.Error(errMsg)

					drsObject, errG := drsmap.DrsInfoFromOid(downloadMsg.Oid)
					if errG == nil && drsObject != nil {
						manualDownloadMsg := fmt.Sprintf("%s %s", drsObject.AccessMethods[0].AccessURL.URL, drsObject.Name)
						logger.Info(manualDownloadMsg)
						lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 302, manualDownloadMsg)
					} else {
						logger.Error(fmt.Sprintf("drsClient.GetObject failed for %s: %v ", downloadMsg.Oid, errG))
						lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
					}
					continue
				}
				if matchingRecord == nil {
					errMsg := fmt.Sprintf("No matching record found for project %s and OID %s", drsClient.GetProjectId(), downloadMsg.Oid)
					logger.ErrorContext(ctx, errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 404, errMsg)
					continue
				}

				// download using data-client
				dstPath, err := drsmap.GetObjectPath(common.LFS_OBJS_PATH, downloadMsg.Oid)
				if err != nil {
					errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
					logger.ErrorContext(ctx, errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 400, errMsg)
					continue
				}

				err = drsClient.DownloadFile(
					ctx,
					matchingRecord.Id,
					dstPath,
				)
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading file for OID %s (GUID: %s): %v", downloadMsg.Oid, matchingRecord.Id, err)
					logger.ErrorContext(ctx, errMsg)
					lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
					continue
				}

				lfs.WriteProgressMessage(streamEncoder, downloadMsg.Oid, downloadMsg.Size, downloadMsg.Size)

				// send success message back
				logger.InfoContext(ctx, fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))

				lfs.WriteCompleteMessage(streamEncoder, downloadMsg.Oid, dstPath)

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event
				logger.Debug("Upload requested")

				// create UploadMessage from the received message
				var uploadMsg lfs.UploadMessage
				if err := sConfig.Unmarshal(scanner.Bytes(), &uploadMsg); err != nil {
					errMsg := fmt.Sprintf("Error parsing UploadMessage: %v", err)
					logger.Error(errMsg)
					lfs.WriteErrorMessage(streamEncoder, uploadMsg.Oid, 400, errMsg)
					continue
				}

				ctx := dataClientCommon.WithProgress(context.Background(), lfs.NewProgressCallback(streamEncoder))
				ctx = dataClientCommon.WithOid(ctx, uploadMsg.Oid)
				logger.InfoContext(ctx, fmt.Sprintf("Uploading file OID %s", uploadMsg.Oid))

				drsObj, err := drsClient.RegisterFile(ctx, uploadMsg.Oid, uploadMsg.Path)
				if err != nil {
					errMsg := fmt.Sprintf("Error registering file: %v\n", err)
					logger.ErrorContext(ctx, errMsg)
					lfs.WriteErrorMessage(streamEncoder, uploadMsg.Oid, 502, errMsg)
					continue
				}
				// send success message back
				lfs.WriteCompleteMessage(streamEncoder, uploadMsg.Oid, drsObj.Name)
				logger.InfoContext(ctx, fmt.Sprintf("Upload for Oid %s complete", uploadMsg.Oid))

			} else if evt, ok := msg["event"]; ok && evt == "terminate" {
				logger.Info("LFS transfer terminate received.")
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Error(fmt.Sprintf("stdin error: %s", err))
		}

		logger.Info("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return nil

	},
}
