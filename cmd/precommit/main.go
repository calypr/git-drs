package precommit

import (
	"fmt"
	"log"

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
	Long:  "Pre-commit hook that creates and commits a DRS object to the repo for every LFS file committed",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// set up logger
		myLogger, err := client.NewLogger("")
		if err != nil {
			log.Printf("Failed to open log file: %v", err)
			return err
		}
		defer myLogger.Close()

		myLogger.Log("~~~~~~~~~~~~~ START: pre-commit ~~~~~~~~~~~~~")

		err = client.UpdateDrsObjects()
		if err != nil {
			fmt.Println("UpdateDrsObjects failed: hello:", err)
			return err
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: pre-commit ~~~~~~~~~~~~~")
		return nil
	},
}
