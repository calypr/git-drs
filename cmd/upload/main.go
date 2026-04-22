package upload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitdrsdrs "github.com/calypr/git-drs/client/drs"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	xferupload "github.com/calypr/syfon/client/xfer/upload"
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

		remoteConfig := config.GetRemote(remoteName)
		organization := ""
		projectID := ""
		storagePrefix := ""
		bucketName := ""
		if remoteConfig != nil {
			organization = remoteConfig.GetOrganization()
			projectID = remoteConfig.GetProjectId()
			storagePrefix = remoteConfig.GetStoragePrefix()
			bucketName = remoteConfig.GetBucketName()
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

				objs, err := gitdrsdrs.GetObjectByHashForGit(cmd.Context(), client.API, sha256, organization, projectID)
				if err != nil || len(objs) == 0 {
					did := sha256
					name := filepath.Base(src)
					drsObj, err := gitdrsdrs.BuildDrsObj(name, sha256, s.Size(), did, bucketName, organization, projectID, storagePrefix)
					if err != nil {
						return fmt.Errorf("build DRS object for %s: %w", src, err)
					}
					registered, err := xferupload.RegisterFile(cmd.Context(), client.API.SyfonClient().Data(), client.API.SyfonClient().DRS(), drsObj, src, bucketName)
					if err != nil {
						return fmt.Errorf("error uploading %s: %v", src, err)
					}
					if registered != nil {
						logger.Info(fmt.Sprintf("Successfully uploaded %s to server with DRS ID %s", src, registered.Id))
					}
				} else {
					logger.Info(fmt.Sprintf("File %s already exists on server with DRS ID %s, skipping upload", src, strings.TrimSpace(objs[0].Id)))
				}
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
}
