package download

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/projectdir"
	"github.com/calypr/git-drs/s3_utils"
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
	Use:   "download <remote> <oid>",
	Short: "Download file using file object ID",
	Long:  "Download file using file object ID (sha256 hash). Use lfs ls-files to get oid",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		remote, oid := args[0], args[1]

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		drsClient, err := cfg.GetRemoteClient(config.Remote(remote), logger)
		if err != nil {
			logger.Printf("\nerror creating DRS client: %s", err)
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
		err = s3_utils.DownloadSignedUrl(accessUrl.URL, dstPath)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s: %v", oid, err)
		}

		if err != nil {
			return fmt.Errorf("\nerror downloading file object ID %s: %s", oid, err)
		}

		logger.Print("file downloaded")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
