package prepush

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "pre-push-prepare",
	Short: "pre-push hook to update DRS objects",
	Long:  "Pre-push hook that updates DRS objects before transfer",
	Args:  cobra.RangeArgs(0, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		//myLogger := drslog.GetLogger()
		myLogger, err := drslog.NewLogger("", false)
		if err != nil {
			return fmt.Errorf("error creating logger: %v", err)
		}

		myLogger.Info("~~~~~~~~~~~~~ START: pre-push ~~~~~~~~~~~~~")

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error getting config: %v", err)
		}

		//Command-line arguments: The hook receives two parameters:
		//* The name of the remote (e.g., origin).
		//* The remote's location/URL (e.g., github.com).
		// Create gitRemoteName and gitRemoteLocation from args.
		myLogger.Debug(fmt.Sprintf("pre-push args: %v", args))
		var gitRemoteName, gitRemoteLocation string
		if len(args) >= 1 {
			gitRemoteName = args[0]
		}
		if len(args) >= 2 {
			gitRemoteLocation = args[1]
		}
		if gitRemoteName == "" {
			gitRemoteName = "origin"
		}
		myLogger.Debug(fmt.Sprintf("git remote name: %s, git remote location: %s", gitRemoteName, gitRemoteLocation))

		// get the default remote from the .git/drs/config
		var remote config.Remote
		remote, err = cfg.GetDefaultRemote()
		if err != nil {
			myLogger.Debug(fmt.Sprintf("Warning. Error getting default remote: %v", err))
			// Print warning to stderr and return success (exit 0)
			fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting default remote:", err)
			return nil
		}

		// get the remote client
		cli, err := cfg.GetRemoteClient(remote, myLogger)
		if err != nil {
			// Print warning to stderr and return success (exit 0)
			fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting remote client:", err)
			myLogger.Debug(fmt.Sprintf("Warning. Skipping DRS preparation. Error getting remote client: %v", err))
			return nil
		}

		dc, ok := cli.(*indexd_client.IndexDClient)
		if !ok {
			return fmt.Errorf("cli is not IndexdClient: %T", cli)
		}
		myLogger.Debug(fmt.Sprintf("Current server: %s", dc.ProjectId))

		// Buffer stdin to a temp file and invoke `git lfs pre-push <remote> <url>` with same args and stdin.
		tmp, err := os.CreateTemp("", "prepush-stdin-*")
		if err != nil {
			myLogger.Error(fmt.Sprintf("error creating temp file for stdin: %v", err))
			return err
		}
		defer func() {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}()

		// Copy all of stdin into the temp file.
		if _, err := io.Copy(tmp, os.Stdin); err != nil {
			myLogger.Error(fmt.Sprintf("error buffering stdin: %v", err))
			return err
		}

		// Rewind to start so the child process can read it.
		if _, err := tmp.Seek(0, 0); err != nil {
			myLogger.Error(fmt.Sprintf("error seeking temp stdin: %v", err))
			return err
		}

		// read the temp file and get a list of all unique local branches being pushed
		branches, err := readPushedBranches(tmp)
		if err != nil {
			myLogger.Error(fmt.Sprintf("error reading pushed branches: %v", err))
			return err
		}

		myLogger.Debug(fmt.Sprintf("Preparing DRS objects for push branches: %v", branches))
		err = drsmap.UpdateDrsObjects(cli, gitRemoteName, gitRemoteLocation, branches, myLogger)
		if err != nil {
			myLogger.Error(fmt.Sprintf("UpdateDrsObjects failed: %v", err))
			return err
		}
		myLogger.Info("~~~~~~~~~~~~~ COMPLETED: pre-push ~~~~~~~~~~~~~")
		return nil
	},
}

// readPushedBranches reads git push lines from the provided temp file,
// extracts unique local branch names for refs under `refs/heads/` and
// returns them sorted. The file is rewound to the start before returning.
func readPushedBranches(f *os.File) ([]string, error) {
	// Ensure we read from start
	// example:
	// refs/heads/main 67890abcdef1234567890abcdef1234567890abcd refs/heads/main 12345abcdef67890abcdef1234567890abcdef12
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	set := make(map[string]struct{})
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		localRef := fields[0]
		const prefix = "refs/heads/"
		if strings.HasPrefix(localRef, prefix) {
			branch := strings.TrimPrefix(localRef, prefix)
			if branch != "" {
				set[branch] = struct{}{}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	branches := make([]string, 0, len(set))
	for b := range set {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	// Rewind so caller can reuse the file
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	return branches, nil
}
