package prepush

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
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
		return NewPrePushService().Run(args, os.Stdin)
	},
}

type PrePushService struct {
	newLogger        func(string, bool) (*slog.Logger, error)
	loadConfig       func() (*config.Config, error)
	updateDrsObjects func(drs.ObjectBuilder, string, string, []string, *slog.Logger) error
	createTempFile   func(dir, pattern string) (*os.File, error)
}

func NewPrePushService() *PrePushService {
	return &PrePushService{
		newLogger:        drslog.NewLogger,
		loadConfig:       config.LoadConfig,
		updateDrsObjects: drsmap.UpdateDrsObjects,
		createTempFile:   os.CreateTemp,
	}
}

func (s *PrePushService) Run(args []string, stdin io.Reader) error {
	myLogger, err := s.newLogger("", false)
	if err != nil {
		return fmt.Errorf("error creating logger: %v", err)
	}

	myLogger.Info("~~~~~~~~~~~~~ START: pre-push ~~~~~~~~~~~~~")

	cfg, err := s.loadConfig()
	if err != nil {
		return fmt.Errorf("error getting config: %v", err)
	}

	gitRemoteName, gitRemoteLocation := parseRemoteArgs(args)
	myLogger.Debug(fmt.Sprintf("git remote name: %s, git remote location: %s", gitRemoteName, gitRemoteLocation))

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		myLogger.Debug(fmt.Sprintf("Warning. Error getting default remote: %v", err))
		fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting default remote:", err)
		return nil
	}

	remoteConfig := cfg.GetRemote(remote)
	if remoteConfig == nil {
		fmt.Fprintln(os.Stderr, "Warning. Skipping DRS preparation. Error getting remote configuration.")
		myLogger.Debug("Warning. Skipping DRS preparation. Error getting remote configuration.")
		return nil
	}

	builder := drs.NewObjectBuilder(remoteConfig.GetBucketName(), remoteConfig.GetProjectId())
	myLogger.Debug(fmt.Sprintf("Current server project: %s", builder.ProjectID))

	tmp, err := bufferStdin(stdin, s.createTempFile)
	if err != nil {
		myLogger.Error(fmt.Sprintf("error buffering stdin: %v", err))
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	branches, err := readPushedBranches(tmp)
	if err != nil {
		myLogger.Error(fmt.Sprintf("error reading pushed branches: %v", err))
		return err
	}

	myLogger.Debug(fmt.Sprintf("Preparing DRS objects for push branches: %v", branches))
	err = s.updateDrsObjects(builder, gitRemoteName, gitRemoteLocation, branches, myLogger)
	if err != nil {
		myLogger.Error(fmt.Sprintf("UpdateDrsObjects failed: %v", err))
		return err
	}
	myLogger.Info("~~~~~~~~~~~~~ COMPLETED: pre-push ~~~~~~~~~~~~~")
	return nil
}

func parseRemoteArgs(args []string) (string, string) {
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
	return gitRemoteName, gitRemoteLocation
}

func bufferStdin(stdin io.Reader, createTempFile func(dir, pattern string) (*os.File, error)) (*os.File, error) {
	tmp, err := createTempFile("", "prepush-stdin-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file for stdin: %w", err)
	}

	if _, err := io.Copy(tmp, stdin); err != nil {
		return nil, fmt.Errorf("error buffering stdin: %w", err)
	}

	if _, err := tmp.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("error seeking temp stdin: %w", err)
	}
	return tmp, nil
}

// readPushedBranches reads git push lines from the provided temp file,
// extracts unique local branch names for refs under `refs/heads/` and
// returns them sorted. The file is rewound to the start before returning.
func readPushedBranches(f io.ReadSeeker) ([]string, error) {
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
