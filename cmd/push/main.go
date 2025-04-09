package push

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "push",
	Short: "Push a repo",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		for i := range args {
			fmt.Printf("Pushing %s\n", args[i])
		}
		return nil
	},
}
