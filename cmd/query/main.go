package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var remote string
var checksum = false
var pretty = false

func queryByChecksum(ctx context.Context, gc *config.GitContext, checksum string) ([]drsapi.DrsObject, error) {
	hashType := checksumTypeForString(checksum)
	if hashType != hash.ChecksumTypeSHA256.String() {
		return nil, fmt.Errorf("checksum lookup currently only supports sha256 (got %q); non-sha256 support is tracked in syfon DRSService.GetObjectsByChecksum", hashType)
	}
	return gc.Client.DRS().GetObjectsByHashForResource(ctx, checksum, gc.Organization, gc.ProjectId)
}

func checksumTypeForString(sum string) string {
	switch len(strings.TrimSpace(sum)) {
	case 32:
		return "md5"
	case 40:
		return "sha1"
	case 64:
		return "sha256"
	case 128:
		return "sha512"
	default:
		return string(hash.NormalizeChecksumType(sum))
	}
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "query <drs_id>",
	Short: "Query DRS server by DRS ID",
	Long:  "Query DRS server by DRS ID",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: requires exactly 1 argument (DRS ID), received %d\n\nUsage: %s\n\nSee 'git drs query --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		gc, err := cfg.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}

		if checksum {
			objs, err := queryByChecksum(context.Background(), gc, args[0])
			if err != nil {
				return err
			}
			for _, drsObj := range objs {
				if err := common.PrintDRSObject(drsObj, pretty); err != nil {
					return err
				}
			}
			return nil
		}

		obj, err := gc.Client.DRS().GetObject(context.Background(), args[0])
		if err != nil {
			return err
		}
		return common.PrintDRSObject(obj, pretty)
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().BoolVarP(&checksum, "checksum", "c", checksum, "Find by checksum")
	Cmd.Flags().BoolVarP(&pretty, "pretty", "p", pretty, "Print indented JSON")
}
