package query

import (
	"context"
	"fmt"

	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/pkg/hash"
	"github.com/spf13/cobra"
)

var remote string
var checksum = false
var pretty = false

type checksumClient interface {
	GetObjectByHash(ctx context.Context, hash *hash.Checksum) ([]drs.DRSObject, error)
}

func queryByChecksum(client checksumClient, checksum string) ([]drs.DRSObject, error) {
	// Auto-detect checksum type based on hash length
	checksumType := hash.ChecksumTypeSHA256
	switch len(checksum) {
	case 32:
		// 128-bit / 32-hex-character checksum (e.g., MD5)
		checksumType = hash.ChecksumTypeMD5
	case 40:
		// 160-bit / 40-hex-character checksum (e.g., SHA1)
		checksumType = hash.ChecksumTypeSHA1
	case 64:
		// 256-bit / 64-hex-character checksum (e.g., SHA256)
		checksumType = hash.ChecksumTypeSHA256
	case 128:
		// 512-bit / 128-hex-character checksum (e.g., SHA512)
		checksumType = hash.ChecksumTypeSHA512
	}

	return client.GetObjectByHash(context.Background(), &hash.Checksum{
		Checksum: checksum,
		Type:     string(checksumType),
	})
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

		var obj *drs.DRSObject

		if checksum {
			objs, err := queryByChecksum(client.API, args[0])
			if err != nil {
				return err
			}
			for _, drsObj := range objs {
				if err := common.PrintDRSObject(drsObj, pretty); err != nil {
					return err
				}
			}
		} else {
			obj, err = client.API.GetObject(context.Background(), args[0])
			if err != nil {
				return err
			}
			if err := common.PrintDRSObject(*obj, pretty); err != nil {
				return err
			}
		}
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().BoolVarP(&checksum, "checksum", "c", checksum, "Find by checksum")
	Cmd.Flags().BoolVarP(&pretty, "pretty", "p", pretty, "Print indented JSON")
}
