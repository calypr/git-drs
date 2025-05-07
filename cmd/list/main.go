package list

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/bmeg/git-drs/utils"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "list files",
	Long:    ``,
	Args:    cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		gitTop, err := utils.GitTopLevel()
		if err != nil {
			fmt.Printf("Error: %s\n", err)
			return err
		}
		manifestDir := filepath.Join(gitTop, "MANIFEST")
		fmt.Printf("Manifest: %s\n", manifestDir)
		s, err := os.Stat(manifestDir)
		if err != nil {
			return err
		}
		if s.IsDir() {
			files, err := filepath.Glob(filepath.Join(manifestDir, "*"))
			if err != nil {
				return err
			}
			for _, i := range files {
				fmt.Printf("%s\n", i)
			}
		}
		return nil
	},
}
