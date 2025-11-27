package addref

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <dst path>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object, eg passing a DRS URI from AnVIL. Requires that the sha256 of the file is already in the cache",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsUri := args[0]
		dstPath := args[1]

		logger := drslog.GetLogger()

		logger.Printf("Adding reference to DRS object %s to %s\n", drsUri, dstPath)

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		client, err := cfg.GetCurrentRemoteClient(logger)
		if err != nil {
			return err
		}

		obj, err := client.GetObject(drsUri)
		if err != nil {
			return err
		}
		objSha := ""
		for _, i := range obj.Checksums {
			if i.Type == drs.ChecksumTypeSHA256 {
				objSha = i.Checksum
			}
		}
		if objSha == "" {
			return fmt.Errorf("object %s sha256 not avalible", drsUri)
		}
		drsmap.CreateLfsPointer(obj, dstPath)
		return nil
	},
}
