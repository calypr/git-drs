package cmd

import (
	"github.com/calypr/git-drs/cmd/addref"
	"github.com/calypr/git-drs/cmd/addurl"
	"github.com/calypr/git-drs/cmd/cache"
	"github.com/calypr/git-drs/cmd/delete"
	"github.com/calypr/git-drs/cmd/deleteproject"
	"github.com/calypr/git-drs/cmd/download"
	"github.com/calypr/git-drs/cmd/fetch"
	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/cmd/list"
	"github.com/calypr/git-drs/cmd/listconfig"
	"github.com/calypr/git-drs/cmd/prepush"
	"github.com/calypr/git-drs/cmd/push"
	"github.com/calypr/git-drs/cmd/query"
	"github.com/calypr/git-drs/cmd/register"
	"github.com/calypr/git-drs/cmd/remote"
	"github.com/calypr/git-drs/cmd/transfer"
	"github.com/calypr/git-drs/cmd/transferref"
	"github.com/calypr/git-drs/cmd/version"
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
	RootCmd.AddCommand(deleteproject.Cmd)
	RootCmd.AddCommand(register.Cmd)
	RootCmd.AddCommand(download.Cmd)
	RootCmd.AddCommand(initialize.Cmd)
	RootCmd.AddCommand(list.Cmd)
	RootCmd.AddCommand(list.ListProjectCmd)
	RootCmd.AddCommand(listconfig.Cmd)
	RootCmd.AddCommand(prepush.Cmd)
	RootCmd.AddCommand(query.Cmd)
	RootCmd.AddCommand(transfer.Cmd)
	RootCmd.AddCommand(transferref.Cmd)
	RootCmd.AddCommand(version.Cmd)
	RootCmd.AddCommand(addurl.AddURLCmd)
	RootCmd.AddCommand(remote.Cmd)
	RootCmd.AddCommand(fetch.Cmd)
	RootCmd.AddCommand(push.Cmd)

	RootCmd.CompletionOptions.HiddenDefaultCmd = true
	RootCmd.SilenceUsage = true
}
