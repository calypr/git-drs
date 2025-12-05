package deleteproject

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var remote string

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:    "delete-project <project_id>",
	Short:  "Delete all indexd records for a given project",
	Long:   "Delete all indexd records for a given project",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if remote == "" {
			remote = config.ORIGIN
		}

		logger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		drsClient, err := cfg.GetRemoteClient(config.Remote(remote), logger)
		if err != nil {
			logger.Printf("error creating indexd client: %s", err)
			return err
		}

		// Delete the matching record
		err = drsClient.DeleteRecordsByProject(args[1])
		if err != nil {
			return fmt.Errorf("Error deleting project %s: %v", args[0], err)
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "remote calypr instance to use")
}
