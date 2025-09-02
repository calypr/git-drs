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
		var version = "0.2.2-dev"
		fmt.Println("git-drs", version)
	},
}
