package prepush

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "prepush",
	Short: "pre-push hook to update DRS objects",
	Long:  "Pre-push hook that updates DRS objects before transfer",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		//myLogger := drslog.GetLogger()
		myLogger, err := drslog.NewLogger("", false)
		if err != nil {
			return fmt.Errorf("error creating logger: %v", err)
		}

		myLogger.Print("~~~~~~~~~~~~~ START: pre-push ~~~~~~~~~~~~~")

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error getting config: %v", err)
		}

		// Try to prepare DRS objects, but don't fail the entire push if config is incomplete
		var remote config.Remote
		myLogger.Printf("pre-push args: %v", args)
		remote, err = cfg.GetDefaultRemote()
		if err != nil {
			myLogger.Printf("Warning. Error getting default remote: %v", err)
			// Print warning to stderr but continue to git lfs pre-push
			fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting default remote:", err)
		} else {
			cli, err := cfg.GetRemoteClient(remote, myLogger)
			if err != nil {
				// Print warning to stderr but continue to git lfs pre-push
				fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting remote client:", err)
				myLogger.Printf("Warning. Skipping DRS preparation. Error getting remote client: %v", err)
			} else {
				dc, ok := cli.(*indexd_client.IndexDClient)
				if !ok {
					// Unexpected client type - log warning and continue
					fmt.Fprintf(os.Stderr, "Warning. Skipping DRS preparation. Unexpected client type: %T\n", cli)
					myLogger.Printf("Warning. Skipping DRS preparation. Unexpected client type: %T", cli)
				} else {
					myLogger.Printf("Current server: %s", dc.ProjectId)

					myLogger.Printf("Preparing DRS objects for push...\n")
					err = drsmap.UpdateDrsObjects(cli, myLogger)
					if err != nil {
						// DRS preparation failed - log warning and continue with git lfs pre-push
						fmt.Fprintln(os.Stderr, "Warning. DRS object preparation failed:", err)
						myLogger.Print("Warning. DRS object preparation failed:", err)
					} else {
						myLogger.Printf("DRS objects prepared for push!\n")
					}
				}
			}
		}

		// Buffer stdin to a temp file and invoke `git lfs pre-push <remote> <url>` with same args and stdin.
		tmp, err := os.CreateTemp("", "prepush-stdin-*")
		if err != nil {
			myLogger.Printf("error creating temp file for stdin: %v", err)
			return err
		}
		defer func() {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}()

		// Copy all of stdin into the temp file.
		if _, err := io.Copy(tmp, os.Stdin); err != nil {
			myLogger.Printf("error buffering stdin: %v", err)
			return err
		}

		// Rewind to start so the child process can read it.
		if _, err := tmp.Seek(0, 0); err != nil {
			myLogger.Printf("error seeking temp stdin: %v", err)
			return err
		}

		// Build and run: git lfs pre-push <args...>
		cmdArgs := append([]string{"lfs", "pre-push"}, args...)
		myLogger.Printf("running: git %v (stdin buffered)", cmdArgs)

		// Use a different variable name to avoid shadowing the cobra 'cmd' parameter.
		gitCmd := exec.Command("git", cmdArgs...)
		gitCmd.Stdin = tmp
		// Send stdout/stderr to stderr to avoid confusing Git (hook should not emit stdout).
		gitCmd.Stdout = os.Stderr
		gitCmd.Stderr = os.Stderr

		if err := gitCmd.Run(); err != nil {
			myLogger.Printf("git lfs pre-push failed: %v", err)
			return err
		}

		myLogger.Print("git lfs pre-push completed successfully")

		myLogger.Print("~~~~~~~~~~~~~ COMPLETED: pre-push ~~~~~~~~~~~~~")
		return nil
	},
}
