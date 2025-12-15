package transfer

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
	transferOperation string // "upload" or "download"
)

// downloadWorker handles only download requests — SONIC SPEED
func downloadWorker(id int, input <-chan TransferJob, output chan<- TransferResult) {
	myLogger := drslog.GetLogger()
	for job := range input {
		var msg lfs.DownloadMessage
		if err := sonic.ConfigFastest.Unmarshal(job.data, &msg); err != nil {
			errMsg := fmt.Sprintf("Failed to parse download message: %v", err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: "", Error: lfs.Error{Code: 400, Message: errMsg}},
				isError: true,
			}
			continue
		}

		myLogger.Printf("Worker %d: Downloading file OID %s", id, msg.Oid)

		accessUrl, err := job.drsClient.GetDownloadURL(msg.Oid)
		if err != nil {
			errMsg := fmt.Sprintf("Error getting signed URL for OID %s: %v", msg.Oid, err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 502, Message: errMsg}},
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
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 502, Message: errMsg}},
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
		if err := sonic.ConfigFastest.Unmarshal(job.data, &msg); err != nil {
			errMsg := fmt.Sprintf("Failed to parse upload message: %v", err)
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: "", Error: lfs.Error{Code: 400, Message: errMsg}},
				isError: true,
			}
			continue
		}

		myLogger.Printf("Worker %d: Uploading file OID %s", id, msg.Oid)

		drsObj, err := job.drsClient.RegisterFile(msg.Oid)
		if err != nil {
			errMsg := "Error registering file: " + err.Error()
			myLogger.Print(errMsg)
			output <- TransferResult{
				data:    lfs.ErrorMessage{Event: "error", Oid: msg.Oid, Error: lfs.Error{Code: 502, Message: errMsg}},
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
		logger.Print("~~~~~~~~~~~~~ START: drs transfer (SONIC MODE) ~~~~~~~~~~~~~")

		numWorkers := getConcurrentTransfers(logger)

		// Gotta go fast — big buffer
		scanner := bufio.NewScanner(os.Stdin)
		const maxCapacity = 10 * 1024 * 1024 // 10 MB
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, maxCapacity)

		transferQueue := make(chan TransferJob, numWorkers)
		resultQueue := make(chan TransferResult, numWorkers)
		var wg sync.WaitGroup

		// Single writer goroutine — must stay ordered
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			encoder := encoder.NewStreamEncoder(os.Stdout) // Output still uses stdlib (safe & compatible)
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

		// Load config
		cfg, err := config.LoadConfig()
		if err != nil {
			logger.Printf("Error loading config: %v", err)
			return err
		}

		var drsClient client.DRSClient
		var remoteName string

		// --- Handle init message with SONIC ---
		if !scanner.Scan() {
			err := fmt.Errorf("failed to read initial message from stdin")
			logger.Printf("Error: %v", err)
			return err
		}
		initBytes := scanner.Bytes()
		var initMsg lfs.InitMessage
		if err := sonic.ConfigFastest.Unmarshal(initBytes, &initMsg); err != nil {
			logger.Printf("Error decoding initial JSON message: %v", err)
			return err
		}

		if initMsg.Event != "init" {
			err := fmt.Errorf("protocol error: expected 'init' message, got '%s'", initMsg.Event)
			logger.Printf("Error: %v", err)
			return err
		}

		// Determine remote
		if initMsg.Remote != "" {
			remoteName = initMsg.Remote
			logger.Printf("Initializing connection. Remote used: %s", remoteName)
		} else {
			remoteName = config.ORIGIN
			logger.Print("Initializing connection, remote not specified — using origin")
		}
		remote := config.Remote(remoteName)
		drsClient, err = cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Printf("Error creating DRS client: %v", err)
			return err
		}

		// Set operation — this is the law
		if initMsg.Operation == "upload" || initMsg.Operation == "download" {
			transferOperation = initMsg.Operation
			logger.Printf("Transfer operation: %s — GOTTA GO FAST", transferOperation)
		} else {
			err := fmt.Errorf("invalid or missing operation in init message: %s", initMsg.Operation)
			logger.Print(err.Error())
			return err
		}

		// Pre-load DRS map only for uploads
		if transferOperation == "upload" {
			logger.Print("Preparing DRS map for upload operation")
			if err := drsmap.UpdateDrsObjects(drsClient, logger); err != nil {
				logger.Printf("Error updating DRS map: %v", err)
				return err
			}
		}

		// Respond to init
		resultQueue <- TransferResult{data: struct{}{}}

		// Start the correct worker fleet
		workerFunc := downloadWorker
		if transferOperation == "upload" {
			workerFunc = uploadWorker
		}
		for i := range numWorkers { // Fixed: was "range numWorkers"
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				workerFunc(id, transferQueue, resultQueue)
			}(i)
		}

		for scanner.Scan() {
			currentBytes := scanner.Bytes()

			// Ultra-fast terminate check
			if strings.Contains(string(currentBytes), `"event":"terminate"`) {
				logger.Print("Received TERMINATE signal. Draining rings and finishing...")
				break
			}

			// Double-check with Sonic just in case
			var generic struct {
				Event string `json:"event"`
			}
			if sonic.ConfigFastest.Unmarshal(currentBytes, &generic) == nil && generic.Event == "terminate" {
				logger.Print("Confirmed TERMINATE. Shutting down boost...")
				break
			}
			transferQueue <- TransferJob{data: currentBytes, drsClient: drsClient}
		}

		// Cleanup
		if err := scanner.Err(); err != nil {
			logger.Printf("stdin error: %v", err)
		}
		close(transferQueue)
		wg.Wait()
		close(resultQueue)
		<-writerDone

		logger.Print("~~~~~~~~~~~~~ COMPLETED: transfer (ZOOM ZOOM) ~~~~~~~~~~~~~")
		return nil
	},
}

func getConcurrentTransfers(logger *drslog.Logger) int {
	const defaultValue = 8
	cmd := exec.Command("git", "config", "--get", "lfs.concurrenttransfers")
	output, err := cmd.Output()
	if err != nil {
		logger.Printf("Could not read 'lfs.concurrenttransfers' from git config: %v. Using default: %d", err, defaultValue)
		return defaultValue
	}
	s := strings.TrimSpace(string(output))
	if s == "" {
		return defaultValue
	}
	val, err := strconv.Atoi(s)
	if err != nil || val <= 0 {
		logger.Printf("Invalid or zero lfs.concurrenttransfers (%s). Using default: %d", s, defaultValue)
		return defaultValue
	}
	logger.Printf("Using lfs.concurrenttransfers = %d (more rings = more speed)", val)
	return val
}
