package download

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/calypr/data-client/download"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var remote string
var outdir string

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "download <DRS ID>",
	Short: "Download a file from a DRS server",
	Long:  "Download a file from a DRS server, without creating an LFS pointer",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		logger := drslog.GetLogger()

		config, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := config.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		client, err := config.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}
		ctx := context.Background()

		for _, src := range args {
			obj, err := client.GetObject(context.Background(), src)
			if err != nil {
				logger.Error(fmt.Sprintf("Error downloading object %s: %v", src, err))
			} else {
				common.PrintDRSObject(*obj, false)
				dstPath := filepath.Join(outdir, filepath.Base(obj.Name)) //TODO: consider including directory structure in output path
				logger.Info(fmt.Sprintf("Downloading object %s to path %s", src, dstPath))
				err = download.DownloadToPath(
					ctx,
					client.GetGen3Interface(),
					obj.Id,
					dstPath,
				)
				if err != nil {
					logger.Error(fmt.Sprintf("Error downloading object %s to path %s: %v", src, dstPath, err))
				} else {
					logger.Info(fmt.Sprintf("Successfully downloaded object %s to path %s", src, dstPath))
				}
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVarP(&outdir, "outdir", "o", ".", "output directory for downloaded files")
}
