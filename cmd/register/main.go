package register

import (
	"log"

	"github.com/bmeg/git-drs/client"
	"github.com/spf13/cobra"
)

var server string = "https://calypr.ohsu.edu/ga4gh"

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "register",
	Short: "<file>",
	Long:  ``,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Printf("Registering file %s", args[0])
		client, err := client.NewIndexDClient(server)
		if err != nil {
			return err
		}

		//upload the file, name would probably be relative to the base of the git repo
		client.RegisterFile(args[0])

		//remove later
		_ = client

		return nil
	},
}
