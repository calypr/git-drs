package cmd

import (
	"os"

	"github.com/bmeg/git-gen3/cmd/initialize"
	"github.com/bmeg/git-gen3/cmd/list"
	"github.com/bmeg/git-gen3/cmd/pull"
	"github.com/bmeg/git-gen3/cmd/push"
	"github.com/spf13/cobra"
)

// RootCmd represents the root command
var RootCmd = &cobra.Command{
	Use:           "git-gen3",
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		//pre-run code can go here
	},
}

func init() {
	RootCmd.AddCommand(initialize.Cmd)
	RootCmd.AddCommand(push.Cmd)
	RootCmd.AddCommand(pull.Cmd)
	RootCmd.AddCommand(list.Cmd)
	RootCmd.AddCommand(genBashCompletionCmd)
}

var genBashCompletionCmd = &cobra.Command{
	Use:   "bash",
	Short: "Generate bash completions file",
	Run: func(cmd *cobra.Command, args []string) {
		RootCmd.GenBashCompletion(os.Stdout)
	},
}
