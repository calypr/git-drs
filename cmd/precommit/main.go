package precommit

import "github.com/spf13/cobra"

const (
	lfsSpecLine                         = "version https://git-lfs.github.com/spec/v1"
	defaultDirectCommitWarningThreshold = int64(10 * 1024 * 1024)
)

var (
	directCommitWarningThresholdBytes = defaultDirectCommitWarningThreshold
	confirmOversizedDirectGitCommit   = promptOversizedDirectGitCommit
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "precommit",
	Short: "pre-commit hook to update local DRS cache",
	Long:  "Pre-commit hook that updates the local DRS pre-commit cache",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return run(cmd.Context())
	},
}
