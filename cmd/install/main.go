package install

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type gitConfigRunner func(args ...string) error

var runGitConfig gitConfigRunner = defaultGitConfigRunner

var Cmd = NewCommand()

func NewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install git-drs git filter configuration",
		Long: "Description:" +
			"\n  Install global git filter configuration for git-drs",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				cmd.SilenceUsage = false
				return fmt.Errorf("error: accepts no arguments, received %d\n\nUsage: %s\n\nSee 'git drs install --help' for more details", len(args), cmd.UseLine())
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return installGlobalFilterConfig(runGitConfig)
		},
	}
}

func installGlobalFilterConfig(runner gitConfigRunner) error {
	settings := []struct {
		key   string
		value string
	}{
		{key: "filter.drs.clean", value: "git-drs clean -- %f"},
		{key: "filter.drs.smudge", value: "git-drs smudge -- %f"},
		{key: "filter.drs.process", value: "git-drs filter-process"},
		{key: "filter.drs.required", value: "true"},
	}

	for _, setting := range settings {
		if err := runner("config", "--global", setting.key, setting.value); err != nil {
			return err
		}
	}

	return nil
}

func defaultGitConfigRunner(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, trimmed)
	}
	return nil
}
