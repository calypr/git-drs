package transfer

import (
	"bufio"
	"strings"
	"testing"

	"github.com/calypr/git-drs/drslog"
)

func TestEnqueueTransferJobsDrainsTerminate(t *testing.T) {
	input := strings.Join([]string{
		`{"event":"upload","oid":"first"}`,
		`{"event":"terminate"}`,
		`{"event":"terminate"}`,
		`{"event":"upload","oid":"second"}`,
	}, "\n")

	scanner := bufio.NewScanner(strings.NewReader(input))
	transferQueue := make(chan TransferJob, 2)

	err := enqueueTransferJobs(scanner, nil, transferQueue, drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("enqueueTransferJobs returned error: %v", err)
	}

	if got := len(transferQueue); got != 1 {
		t.Fatalf("expected 1 queued transfer, got %d", got)
	}

	job := <-transferQueue
	if !strings.Contains(string(job.data), `"oid":"first"`) {
		t.Fatalf("unexpected job data: %s", string(job.data))
	}
}
