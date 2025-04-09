package pull

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "pull",
	Short: "Pull a file",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for i := range args {
			fmt.Printf("Pulling file %s\n", args[i])
		}
		return nil
	},
}
