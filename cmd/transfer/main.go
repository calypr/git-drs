package transfer

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"

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

// downloadWorker handles only download requests
func downloadWorker(id int, input <-chan TransferJob, output chan<- TransferResult) {
	myLogger := drslog.GetLogger()
	for job := range input {
		var msg lfs.DownloadMessage
		if err := sConfig.Unmarshal(job.data, &msg); err != nil {
			errMsg := fmt.Sprintf("Failed to parse download message: %v", err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 400, Message: errMsg}},
				isError: true,
			}
			continue
		}

		myLogger.Printf("Worker %d: Downloading file OID %s", id, msg.Oid)

		accessUrl, err := job.drsClient.GetDownloadURL(msg.Oid)
		if err != nil {
			errMsg := fmt.Sprintf("Worker %d: Error getting signed URL for OID %s: %v", id, msg.Oid, err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 500, Message: errMsg}},
				isError: true,
			}
			continue
		}
		if accessUrl.URL == "" {
			errMsg := fmt.Sprintf("Empty access URL for OID %s", msg.Oid)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 500, Message: errMsg}},
				isError: true,
			}
			continue
		}

		dstPath, err := drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, msg.Oid)
		if err != nil {
			errMsg := fmt.Sprintf("Error getting destination path for OID %s: %v", msg.Oid, err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 400, Message: errMsg}},
				isError: true,
			}
			continue
		}

		if err = s3_utils.DownloadSignedUrl(accessUrl.URL, dstPath); err != nil {
			errMsg := fmt.Sprintf("Error downloading file for OID %s: %v", msg.Oid, err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 500, Message: errMsg}},
				isError: true,
			}
			continue
		}

		myLogger.Printf("Worker %d: Download complete for OID %s", id, msg.Oid)
		output <- TransferResult{data: lfs.CompleteMessage{
			Event: "complete",
			Oid:   msg.Oid,
			Path:  dstPath,
		}}
	}
}

// uploadWorker handles only upload requests — GOTTA GO FAST
func uploadWorker(id int, input <-chan TransferJob, output chan<- TransferResult) {
	myLogger := drslog.GetLogger()
	for job := range input {
		var msg lfs.UploadMessage
		if err := sConfig.Unmarshal(job.data, &msg); err != nil {
			errMsg := fmt.Sprintf("Failed to parse upload message: %v", err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 400, Message: errMsg}},
				isError: true,
			}
			continue
		}

		myLogger.Printf("Worker %d: Uploading file OID %s", id, msg.Oid)

		drsObj, err := job.drsClient.RegisterFile(msg.Oid)
		if err != nil {
			errMsg := fmt.Sprintf("(Worker %d) Error registering file: %v", id, err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 400, Message: errMsg}},
				isError: true,
			}
			continue
		}

		myLogger.Printf("Worker %d: Upload complete for OID %s", id, msg.Oid)
		output <- TransferResult{data: lfs.CompleteMessage{
			Event: "complete",
			Oid:   msg.Oid,
			Path:  drsObj.Name,
		}}
	}
}

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

		// Read init message
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			logger.Printf("Error: %v", err)
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		initBytes := make([]byte, len(scanner.Bytes()))
		copy(initBytes, scanner.Bytes())
		var initMsg lfs.InitMessage
		if err := sConfig.Unmarshal(initBytes, &initMsg); err != nil {
			logger.Printf("Error decoding initial JSON message: %v", err)
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		if initMsg.Event != "init" {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%s'", initMsg.Event)
			logger.Printf("Error: %v", err)
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		// Use ConcurrentTransfers from init message
		numWorkers := initMsg.ConcurrentTransfers
		if numWorkers <= 0 {
			numWorkers = 4
			logger.Printf("Invalid ConcurrentTransfers (%d), using default: 4", initMsg.ConcurrentTransfers)
		} else {
			logger.Printf("Using %d concurrent workers (from Git LFS)", numWorkers)
		}

		// Create channels with correct sizing
		transferQueue := make(chan TransferJob, numWorkers*4)
		resultQueue := make(chan TransferResult, numWorkers*4)
		var wg sync.WaitGroup

		// Single writer goroutine — must stay ordered
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			encoder := encoder.NewStreamEncoder(os.Stdout)
			for result := range resultQueue {
				if result.isError {
					if errMsg, ok := result.data.(lfs.ErrorMessage); ok {
						lfs.WriteErrorMessage(encoder, errMsg.Oid, errMsg.Error.Code, errMsg.Error.Message)
					}
				} else {
					encoder.Encode(result.data)
				}
			}
		}()

		var drsClient client.DRSClient

		// Load config first
		cfg, err := config.LoadConfig()
		if err != nil {
			logger.Printf("Error loading config: %v", err)
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		// Determine remote
		remote, err := cfg.GetDefaultRemote()
		if err != nil {
			logger.Printf("Error getting default remote: %v", err)
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		drsClient, err = cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Printf("Error creating DRS client: %v", err)
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		// Determine if upload or download
		if initMsg.Operation == OPERATION_UPLOAD || initMsg.Operation == OPERATION_DOWNLOAD {
			transferOperation = initMsg.Operation
			logger.Printf("Transfer operation: %s", transferOperation)
		} else {
			err := fmt.Errorf("invalid or missing operation in init message: %s", initMsg.Operation)
			logger.Print(err.Error())
			lfs.WriteInitErrorMessage(encoder.NewStreamEncoder(os.Stdout), 400, err.Error())
			return err
		}

		// Pre-load DRS map only for uploads - UpdateDrsObjects moved to pre-push hook

		// Respond to init
		resultQueue <- TransferResult{data: struct{}{}, isError: err != nil}

		// Start the correct worker fleet
		workerFunc := downloadWorker
		if transferOperation == OPERATION_UPLOAD {
			workerFunc = uploadWorker
		}
		for i := range numWorkers {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				workerFunc(id, transferQueue, resultQueue)
			}(i)
		}

		for scanner.Scan() {
			currentBytes := make([]byte, len(scanner.Bytes()))
			copy(currentBytes, scanner.Bytes())
			logger.Printf("Current command: %s", string(currentBytes))

			// Ultra-fast terminate check
			if strings.Contains(string(currentBytes), `"event":"terminate"`) {
				logger.Print("Received TERMINATE signal")
				break
			}

			// Double-check with Sonic just in case
			var generic struct {
				Event string `json:"event"`
			}
			if err := sConfig.Unmarshal(currentBytes, &generic); err == nil && generic.Event == "terminate" {
				logger.Print("Confirmed TERMINATE. Shutting down...")
				break
			}
			transferQueue <- TransferJob{data: currentBytes, drsClient: drsClient}
		}

		// Cleanup
		scanErr := scanner.Err()
		close(transferQueue)
		wg.Wait()
		close(resultQueue)
		<-writerDone
		if scanErr != nil {
			logger.Printf("stdin error: %v", scanErr)
			return scanErr
		}

		logger.Print("~~~~~~~~~~~~~ COMPLETED: custom transfer ~~~~~~~~~~~~~")
		return nil
	},
}
