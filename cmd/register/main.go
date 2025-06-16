package register

import (
	"fmt"
	"log"

	"github.com/bmeg/git-drs/client"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "register",
	Short: "<hash>",
	Long:  `accepts one parameter: <file-hash>`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		log.Printf("Registering file %s", args[0])

		cfg, err := client.LoadConfig()
		if err != nil {
			fmt.Println("error loading config:", err)
			return err
		}
		client, err := client.NewIndexDClient(cfg.QueryServer.BaseURL)
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
