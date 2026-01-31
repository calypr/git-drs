package download

import (
	"fmt"

	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/projectdir"
	"github.com/spf13/cobra"
)

var (
	dstPath string
	remote  string
)

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "download <oid>",
	Short: "Download file using file object ID",
	Long:  "Download file using file object ID (sha256 hash). Use lfs ls-files to get oid",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 1 argument (file object ID), received %d\n\nUsage: %s\n\nSee 'git drs download --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		oid := args[0]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			return fmt.Errorf("error getting default remote: %v", err)
		}

		drsClient, err := cfg.GetRemoteClient(remoteName, logger)
		if err != nil {
			logger.Error(fmt.Sprintf("\nerror creating DRS client: %s", err))
			return err
		}

		// get signed url
		accessUrl, err := drsClient.GetDownloadURL(oid)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s: %v", oid, err)
		}
		if accessUrl.URL == "" {
			return fmt.Errorf("Unable to get access URL %s", oid)
		}

		// download url to destination path or LFS objects if not specified
		if dstPath == "" {
			dstPath, err = drsmap.GetObjectPath(projectdir.LFS_OBJS_PATH, oid)
		}
		if err != nil {
			return fmt.Errorf("Error getting destination path for OID %s: %v", oid, err)
		}
		err = cloud.DownloadSignedUrl(accessUrl.URL, dstPath)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s: %v", oid, err)
		}

		if err != nil {
			return fmt.Errorf("\nerror downloading file object ID %s: %s", oid, err)
		}

		logger.Debug("file downloaded")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
