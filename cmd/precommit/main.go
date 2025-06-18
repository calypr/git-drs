package precommit

import (
	"fmt"
	"log"
	"os"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/drs"
	"github.com/spf13/cobra"
)

var (
	server  string
	dstPath string
	drsObj  *drs.DRSObject
)

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "precommit",
	Short: "pre-commit hook to create DRS objects",
	Long:  "Pre-commit hook that creates DRS objects based on LFS files in the repo. Stores it to a drs-map.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 0 {
			fmt.Fprintln(os.Stderr, "This command does not take any arguments.")
			os.Exit(1)
		}

		myLogger, err := client.NewLogger("")
		if err != nil {
			// Handle error (e.g., print to stderr and exit)
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer myLogger.Close() // Ensures cleanup

		myLogger.Log("~~~~~~~~~~~~~ START: pre-commit ~~~~~~~~~~~~~")

		err = client.UpdateDrsMap()
		if err != nil {
			fmt.Println("updateDrsMap failed:", err)
			log.Fatalf("updateDrsMap failed: %v", err)
			return err
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: pre-commit ~~~~~~~~~~~~~")
		return nil
	},
}
