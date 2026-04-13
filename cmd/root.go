package cmd

import (
	"github.com/calypr/git-drs/cmd/addref"
	"github.com/calypr/git-drs/cmd/addurl"
	"github.com/calypr/git-drs/cmd/bucket"
	deleteCmd "github.com/calypr/git-drs/cmd/delete"
	"github.com/calypr/git-drs/cmd/deleteproject"

	//"github.com/calypr/git-drs/cmd/download"
	"github.com/calypr/git-drs/cmd/fetch"
	"github.com/calypr/git-drs/cmd/filter"
	"github.com/calypr/git-drs/cmd/initialize"
	"github.com/calypr/git-drs/cmd/install"

	//"github.com/calypr/git-drs/cmd/list"
	//"github.com/calypr/git-drs/cmd/lsfiles"
	"github.com/calypr/git-drs/cmd/precommit"
	"github.com/calypr/git-drs/cmd/prepush"
	"github.com/calypr/git-drs/cmd/pull"
	"github.com/calypr/git-drs/cmd/push"
	"github.com/calypr/git-drs/cmd/query"
	"github.com/calypr/git-drs/cmd/remote"
	"github.com/calypr/git-drs/cmd/track"
	"github.com/calypr/git-drs/cmd/untrack"

	//"github.com/calypr/git-drs/cmd/upload"
	"github.com/calypr/git-drs/cmd/version"
	"github.com/spf13/cobra"
)

// RootCmd represents the root command
var RootCmd = &cobra.Command{
	Use:   "git-drs",
	Short: "Git DRS - Git-LFS file management for DRS servers",
	Long:  "Git DRS provides the benefits of Git-LFS file management using DRS for seamless integration with Gen3 servers",
}

func init() {
	// Hide internal commands
	precommit.Cmd.Hidden = true
	prepush.Cmd.Hidden = true
	filter.Cmd.Hidden = true

	RootCmd.AddCommand(initialize.Cmd)
	RootCmd.AddCommand(version.Cmd)
	RootCmd.AddCommand(filter.Cmd)
	RootCmd.AddCommand(remote.Cmd)
	RootCmd.AddCommand(fetch.Cmd)
	RootCmd.AddCommand(pull.Cmd)
	RootCmd.AddCommand(push.Cmd)
	RootCmd.AddCommand(precommit.Cmd)
	RootCmd.AddCommand(prepush.Cmd)
	RootCmd.AddCommand(addref.Cmd)
	RootCmd.AddCommand(addurl.Cmd)
	RootCmd.AddCommand(deleteCmd.Cmd)
	RootCmd.AddCommand(deleteproject.Cmd)
	RootCmd.AddCommand(query.Cmd)
	RootCmd.AddCommand(bucket.Cmd)
	RootCmd.AddCommand(track.Cmd)
	RootCmd.AddCommand(untrack.Cmd)
	//RootCmd.AddCommand(list.Cmd)
	//RootCmd.AddCommand(lsfiles.Cmd)
	//RootCmd.AddCommand(upload.Cmd)
	//RootCmd.AddCommand(download.Cmd)
	RootCmd.AddCommand(install.Cmd)

	RootCmd.CompletionOptions.HiddenDefaultCmd = true
	RootCmd.SilenceUsage = true
}
