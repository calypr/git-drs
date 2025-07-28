package version

import (
	"fmt"

	"github.com/calypr/git-drs/version"
	"github.com/spf13/cobra"
)

// Cmd represents the "version" command
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Get version",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.String())
	},
}
