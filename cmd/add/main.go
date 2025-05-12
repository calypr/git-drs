package add

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "add",
	Short: "Add a file",
	Long:  ``,
	Args:  cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, fileArg := range args {
			matches, err := filepath.Glob(fileArg)
			if err == nil {
				for _, f := range matches {
					fmt.Printf("Adding %s\n", f)
				}
			}
		}
		return nil
	},
}
