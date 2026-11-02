package auth

import "github.com/spf13/cobra"

var Cmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for transfer providers",
}

func init() {
	Cmd.AddCommand(GlobusCmd)
}
