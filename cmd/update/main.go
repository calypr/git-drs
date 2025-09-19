package update

import (
	"github.com/calypr/git-drs/cmd/update/drs"
	"github.com/calypr/git-drs/cmd/update/lfs"
	"github.com/calypr/git-drs/cmd/update/self"
	"github.com/spf13/cobra"
)

// Cmd is the parent "update" command
var Cmd = &cobra.Command{
	Use:   "update",
	Short: "Update git-drs and dependencies",
	Long:  "Update git-drs itself or dependencies like git-lfs.",
}

func init() {
	// Add subcommands here
	Cmd.AddCommand(drs.Cmd)
	Cmd.AddCommand(lfs.Cmd)
	Cmd.AddCommand(self.Cmd)
}
