package version

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd represents the "version" command
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Get version",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		var version = "0.3.0-rc0"
		fmt.Println("git-drs", version)
	},
}
