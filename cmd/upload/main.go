package upload

import (
	"context"
	"fmt"
	"os"

	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/syfon/client/pkg/hash"
	"github.com/spf13/cobra"
)

var remote string

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "upload <src file>",
	Short: "Upload a file to a DRS server",
	Long:  "Upload a file to a DRS server, without creating an LFS pointer",
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

		for _, src := range args {
			if s, err := os.Stat(src); err != nil {
				logger.Error(fmt.Sprintf("Error stating file %s: %v", src, err))
				return err
			} else if s.IsDir() {
				logger.Error(fmt.Sprintf("Skipping directory %s", src))
				continue
			} else {
				sha256, err := common.CalculateFileSHA256(src)
				if err != nil {
					logger.Error(fmt.Sprintf("Error calculating SHA256 for file %s: %v", src, err))
					return err
				}

				objs, err := client.API.GetObjectByHash(context.Background(), &hash.Checksum{
					Checksum: sha256,
					Type:     string(hash.ChecksumTypeSHA256),
				})
				if err != nil || len(objs) == 0 {
					if obj, err := client.API.RegisterFile(cmd.Context(), sha256, src); err != nil {
						return fmt.Errorf("error uploading %s: %v", src, err)
					} else {
						logger.Info(fmt.Sprintf("Successfully uploaded %s to server with DRS ID %s", src, obj.Id))
					}
				} else {
					logger.Info(fmt.Sprintf("File %s already exists on server with DRS ID %s, skipping upload", src, objs[0].Id))
				}
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
}
