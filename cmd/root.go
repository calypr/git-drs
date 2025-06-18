package cmd

import (
	"os"

	"github.com/bmeg/git-drs/cmd/add"
	"github.com/bmeg/git-drs/cmd/download"
	"github.com/bmeg/git-drs/cmd/filterprocess"
	"github.com/bmeg/git-drs/cmd/initialize"
	"github.com/bmeg/git-drs/cmd/list"
	"github.com/bmeg/git-drs/cmd/precommit"
	"github.com/bmeg/git-drs/cmd/pull"
	"github.com/bmeg/git-drs/cmd/push"
	"github.com/bmeg/git-drs/cmd/query"
	"github.com/bmeg/git-drs/cmd/register"
	"github.com/bmeg/git-drs/cmd/transfer"
	"github.com/spf13/cobra"
)

// RootCmd represents the root command
var RootCmd = &cobra.Command{
	Use:           "git-drs",
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		//pre-run code can go here
	},
}

func init() {
	RootCmd.AddCommand(add.Cmd)
	RootCmd.AddCommand(download.Cmd)
	RootCmd.AddCommand(filterprocess.Cmd)
	RootCmd.AddCommand(genBashCompletionCmd)
	RootCmd.AddCommand(initialize.Cmd)
	RootCmd.AddCommand(list.Cmd)
	RootCmd.AddCommand(precommit.Cmd)
	RootCmd.AddCommand(push.Cmd)
	RootCmd.AddCommand(pull.Cmd)
	RootCmd.AddCommand(query.Cmd)
	RootCmd.AddCommand(register.Cmd)
	RootCmd.AddCommand(transfer.Cmd)
}

var genBashCompletionCmd = &cobra.Command{
	Use:   "bash",
	Short: "Generate bash completions file",
	Run: func(cmd *cobra.Command, args []string) {
		RootCmd.GenBashCompletion(os.Stdout)
	},
}
