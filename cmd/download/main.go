package download

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsremote"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sydownload "github.com/calypr/syfon/client/transfer/download"
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
		for _, src := range args {
			obj, err := client.Client.DRS().GetObject(context.Background(), src)
			if err != nil {
				logger.Error(fmt.Sprintf("Error downloading object %s: %v", src, err))
			} else {
				common.PrintDRSObject(obj, false)
				dstName := src
				if obj.Name != nil && *obj.Name != "" {
					dstName = filepath.Base(*obj.Name)
				}
				dstPath := filepath.Join(outdir, dstName)
				logger.Info(fmt.Sprintf("Downloading object %s to path %s", src, dstPath))
				accessURL, err := resolveAccessURL(cmd.Context(), client, obj)
				if err != nil {
					logger.Error(fmt.Sprintf("Error resolving access URL for object %s: %v", src, err))
					continue
				}
				if err := drsremote.DownloadResolvedToPath(cmd.Context(), client, obj.Id, dstPath, &obj, accessURL, sydownload.DownloadOptions{
					MultipartThreshold: 5 * 1024 * 1024,
					Concurrency:        2,
					ChunkSize:          64 * 1024 * 1024,
				}); err != nil {
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

func resolveAccessURL(ctx context.Context, remote *config.GitContext, obj drsapi.DrsObject) (*drsapi.AccessURL, error) {
	if remote == nil || remote.Client == nil {
		return nil, fmt.Errorf("DRS client unavailable")
	}
	if obj.AccessMethods == nil || len(*obj.AccessMethods) == 0 {
		return nil, fmt.Errorf("no access methods available for DRS object %s", obj.Id)
	}
	accessType := strings.TrimSpace(string((*obj.AccessMethods)[0].Type))
	if accessType == "" {
		return nil, fmt.Errorf("no access type found in access method for DRS object %s", obj.Id)
	}
	accessURL, err := remote.Client.DRS().GetAccessURL(ctx, obj.Id, accessType)
	if err != nil {
		return nil, err
	}
	return &accessURL, nil
}
