package cmd

import (
	"github.com/calypr/git-drs/cmd/addref"
	"github.com/calypr/git-drs/cmd/cache"
	"github.com/calypr/git-drs/cmd/delete"
	"github.com/calypr/git-drs/cmd/download"
	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/cmd/list"
	"github.com/calypr/git-drs/cmd/precommit"
	"github.com/calypr/git-drs/cmd/query"
	"github.com/calypr/git-drs/cmd/transfer"
	"github.com/calypr/git-drs/cmd/transferref"
	"github.com/calypr/git-drs/cmd/workflow"
	"github.com/spf13/cobra"
)

// RootCmd represents the root command
var RootCmd = &cobra.Command{
	Use:   "git-drs",
	Short: "Git DRS - Git-LFS file management for DRS servers",
	Long:  "Git DRS provides the benefits of Git-LFS file management using DRS for seamless integration with Gen3 servers",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		//pre-run code can go here
	},
}

func init() {
	RootCmd.AddCommand(addref.Cmd)
	RootCmd.AddCommand(cache.Cmd)
	RootCmd.AddCommand(delete.Cmd)
	RootCmd.AddCommand(download.Cmd)
	RootCmd.AddCommand(initialize.Cmd)
	RootCmd.AddCommand(list.Cmd)
	RootCmd.AddCommand(list.ListProjectCmd)
	RootCmd.AddCommand(precommit.Cmd)
	RootCmd.AddCommand(query.Cmd)
	RootCmd.AddCommand(transfer.Cmd)
	RootCmd.AddCommand(transferref.Cmd)
	RootCmd.AddCommand(workflow.Cmd)
	RootCmd.CompletionOptions.HiddenDefaultCmd = true
	RootCmd.SilenceUsage = true
}
