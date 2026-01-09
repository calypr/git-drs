package transfer

import (
	"bufio"
	"strings"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drslog"
)

func enqueueTransferJobs(scanner *bufio.Scanner, drsClient client.DRSClient, transferQueue chan<- TransferJob, logger *drslog.Logger) error {
	terminateSeen := false
	for scanner.Scan() {
		currentBytes := make([]byte, len(scanner.Bytes()))
		copy(currentBytes, scanner.Bytes())

		// Ultra-fast terminate check
		if strings.Contains(string(currentBytes), `"event":"terminate"`) {
			if !terminateSeen {
				logger.Print("Received TERMINATE signal; draining stdin")
				terminateSeen = true
			}
			continue
		}

		// Double-check with Sonic just in case
		var generic struct {
			Event string `json:"event"`
		}
		if err := sConfig.Unmarshal(currentBytes, &generic); err == nil && generic.Event == "terminate" {
			if !terminateSeen {
				logger.Print("Confirmed TERMINATE. Draining stdin...")
				terminateSeen = true
			}
			continue
		}
		if terminateSeen {
			continue
		}
		transferQueue <- TransferJob{data: currentBytes, drsClient: drsClient}
	}

	return scanner.Err()
}
