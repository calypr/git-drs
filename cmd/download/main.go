package download

import (
	"context"
	"fmt"

	dataClientCommon "github.com/calypr/data-client/common"
	"github.com/calypr/data-client/download"
	"github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
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

		// get the matching record for this OID
		checksumSpec := &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid}
		records, err := drsClient.GetObjectByHash(context.Background(), checksumSpec)
		if err != nil {
			return fmt.Errorf("Error looking up OID %s: %v", oid, err)
		}

		matchingRecord, err := drsmap.FindMatchingRecord(records, drsClient.GetProjectId())
		if err != nil {
			return fmt.Errorf("Error finding matching record for project %s: %v", drsClient.GetProjectId(), err)
		}
		if matchingRecord == nil {
			return fmt.Errorf("No matching record found for project %s and OID %s", drsClient.GetProjectId(), oid)
		}

		// download url to destination path or LFS objects if not specified
		if dstPath == "" {
			dstPath, err = drsmap.GetObjectPath(common.LFS_OBJS_PATH, oid)
		}
		if err != nil {
			return fmt.Errorf("Error getting destination path for OID %s: %v", oid, err)
		}

		ctx := dataClientCommon.WithOid(context.Background(), oid)
		err = download.DownloadToPath(
			ctx,
			drsClient.GetGen3Interface(),
			matchingRecord.Id,
			dstPath,
		)
		if err != nil {
			return fmt.Errorf("Error downloading file for OID %s (GUID: %s): %v", oid, matchingRecord.Id, err)
		}

		logger.Debug("file downloaded")

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVarP(&dstPath, "dst", "d", "", "Destination path to save the downloaded file")
}
