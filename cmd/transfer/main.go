package transfer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	dataClientCommon "github.com/calypr/data-client/common"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/common"

	"github.com/calypr/data-client/download"
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

				matchingRecord, err := drsmap.FindMatchingRecord(records, drsClient.GetProjectId())
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

				logger.InfoContext(ctx, fmt.Sprintf("Found matching record with ID %s for OID %s %v", matchingRecord.Id, downloadMsg.Oid, matchingRecord.Aliases))

				// download directly when record points to an external remote URL.
				downloadUsingDataClient := true
				for _, alias := range matchingRecord.Aliases {
					if alias == "git-drs-remote-url:true" {
						logger.InfoContext(ctx, fmt.Sprintf(
							"OID %s marked as remote URL (alias=%q); downloading directly. (not using server)",
							downloadMsg.Oid,
							alias,
						))
						downloadUsingDataClient = false
						break
					}
				}

				dstPath, err := drsmap.GetObjectPath(common.LFS_OBJS_PATH, downloadMsg.Oid)

				if downloadUsingDataClient {

					// download using data-client
					if err != nil {
						errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", downloadMsg.Oid, err)
						logger.ErrorContext(ctx, errMsg)
						lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 400, errMsg)
						continue
					}

					err = download.DownloadToPath(
						ctx,
						drsClient.GetGen3Interface(),
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

				} else {
					computedSHA, dstPath, err := cloud.Download(ctx, matchingRecord)
					if err != nil {
						errMsg := fmt.Sprintf("Error downloading file from remote url %s oid %s (GUID: %s): %v", matchingRecord.AccessMethods[0].AccessURL, downloadMsg.Oid, matchingRecord.Id, err)
						logger.ErrorContext(ctx, errMsg)
						lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
						continue
					}
					logger.InfoContext(ctx, fmt.Sprintf("Successfully downloaded OID %s from remote URL to %s", downloadMsg.Oid, dstPath))

					if computedSHA != matchingRecord.Checksums.SHA256 {
						// we have a sentinel file with the ETag as content, but the computed SHA256 doesn't match the expected value from the DRS objec
						// write the sentinel file
						logger.InfoContext(ctx, fmt.Sprintf("computedSHA %s does not match expected SHA256 %s for OID %s", computedSHA, matchingRecord.Checksums.SHA256, downloadMsg.Oid))
						shaOfETag := cloud.GetSHA256(matchingRecord.Checksums.ETag)
						if shaOfETag != matchingRecord.Checksums.SHA256 {
							errMsg := fmt.Sprintf("shaOfETag %s does not match matchingRecord.Checksums.SHA256 %s",
								shaOfETag, matchingRecord.Checksums.SHA256)
							logger.ErrorContext(ctx, errMsg)
							lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
							continue
						}
						logger.InfoContext(ctx, fmt.Sprintf("shaOfETag == computedSHA %s %s", shaOfETag, matchingRecord.Name))

						// the file should already exist as a pointer
						if _, err := os.Stat(matchingRecord.Name); err != nil {
							errMsg := fmt.Sprintf("stat %s: %v", matchingRecord.Name, err)
							logger.ErrorContext(ctx, errMsg)
							lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
							continue
						}
						// write the 'real file' to the expected location in the .git/lfs/objects directory
						if err := os.Rename(dstPath, matchingRecord.Name); err != nil {
							errMsg := fmt.Sprintf("rename %s %s: %v", dstPath, matchingRecord.Name, err)
							logger.ErrorContext(ctx, errMsg)
							lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
							continue
						}

						// write the sentinel data to expected place ( the sha of the sentinel data )
						sentinelData := []byte(matchingRecord.Checksums.ETag)
						_, lfsRoot, _ := lfs.GetGitRootDirectories(ctx)

						oid := shaOfETag // sha of sentinel drsObj.Checksums.SHA256
						dstDir := filepath.Join(lfsRoot, "objects", oid[0:2], oid[2:4])
						dstPath := filepath.Join(dstDir, oid)

						if err := os.MkdirAll(dstDir, 0755); err != nil {
							errMsg := fmt.Sprintf("mkdir %s %v", dstDir, err)
							logger.ErrorContext(ctx, errMsg)
							lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
							continue
						}

						if err := os.WriteFile(dstPath, sentinelData, 0644); err != nil {
							errMsg := fmt.Sprintf("write sentinel to %s: %v", dstPath, err)
							logger.ErrorContext(ctx, errMsg)
							lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
							continue
						}
						logger.InfoContext(ctx, fmt.Sprintf("wrote sentinelData %s to %s", sentinelData, dstPath))

						// now create a drsObject for the new "real" data
						// ensure that it has the url and aliases (flags) from the original
						projectId := drsClient.GetProjectId()
						uuid := drsmap.DrsUUID(projectId, computedSHA)
						realDRSObject := drs.DRSObject{
							Id:            uuid,
							Name:          matchingRecord.Name,
							AccessMethods: matchingRecord.AccessMethods,
							Checksums:     hash.HashInfo{SHA256: computedSHA, ETag: matchingRecord.Checksums.ETag},
							Size:          matchingRecord.Size,
							Aliases:       matchingRecord.Aliases,
						}
						err = lfs.WriteObject(common.DRS_OBJS_PATH, &realDRSObject, computedSHA)
						if err != nil {
							errMsg := fmt.Sprintf("error writing DRS object for oid %s: %v", computedSHA, err)
							logger.ErrorContext(ctx, errMsg)
							lfs.WriteErrorMessage(streamEncoder, downloadMsg.Oid, 502, errMsg)
							continue
						}

					}
				}

				// send success message back
				logger.InfoContext(ctx, fmt.Sprintf("Download for OID %s complete", downloadMsg.Oid))

				lfs.WriteCompleteMessage(streamEncoder, downloadMsg.Oid, dstPath)

			} else if evt, ok := msg["event"]; ok && evt == "upload" {
				// Handle upload event

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
