package addref

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/lfs"
	"github.com/spf13/cobra"
)

var remote string
var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <dst path>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object via URI. Requires that the sha256 of the file is already in the cache",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsUri := args[0]
		dstPath := args[1]

		logger := drslog.GetLogger()

		logger.Debug(fmt.Sprintf("Adding reference to DRS object %s to %s", drsUri, dstPath))

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		client, err := cfg.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}

		obj, err := client.API.GetObject(context.Background(), drsUri)
		if err != nil {
			return err
		}
		dirPath := filepath.Dir(dstPath)
		_, err = os.Stat(dirPath)
		if os.IsNotExist(err) {
			// The directory does not exist
			os.MkdirAll(dirPath, os.ModePerm)
		}

		err = lfs.CreateLfsPointer(obj, dstPath)
		return err
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
}
