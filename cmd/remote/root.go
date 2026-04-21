package remote

import (
	"github.com/calypr/git-drs/cmd/remote/add"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage remote DRS server configs",
}

func init() {
	Cmd.AddCommand(add.Cmd)
	Cmd.AddCommand(ListCmd)
	Cmd.AddCommand(SetCmd)
	Cmd.AddCommand(RemoveCmd)
}
