package initialize

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a repo",
	Long:  ``,
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Running init\n")
		return nil
	},
}
