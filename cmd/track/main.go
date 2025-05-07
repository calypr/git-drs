package track

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "track",
	Short: "Set a file track filter",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for i := range args {
			fmt.Printf("Track %s\n", args[i])
		}
		return nil
	},
}
