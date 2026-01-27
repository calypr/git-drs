package addref

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calypr/data-client/indexd/drs"
	hash "github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var remote string
var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <dst path>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object, eg passing a DRS URI from AnVIL. Requires that the sha256 of the file is already in the cache",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		drsUri := args[0]
		dstPath := args[1]

		logger := drslog.GetLogger()

		logger.Debug(fmt.Sprintf("Adding reference to DRS object %s to %s", drsUri, dstPath))

		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := cfg.GetRemoteOrDefault(remote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		client, err := cfg.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}

		obj, err := client.GetObject(context.Background(), drsUri)
		if err != nil {
			return err
		}
		objSha := ""
		for sumType, sum := range hash.ConvertHashInfoToMap(obj.Checksums) {
			if sumType == hash.ChecksumTypeSHA256.String() {
				objSha = sum
			}
		}
		if objSha == "" {
			return fmt.Errorf("object %s sha256 not available", drsUri)
		}
		dirPath := filepath.Dir(dstPath)
		_, err = os.Stat(dirPath)
		if os.IsNotExist(err) {
			// The directory does not exist
			os.MkdirAll(dirPath, os.ModePerm)
		}

		err = CreateLfsPointer(obj, dstPath)
		return err
	},
}

func CreateLfsPointer(drsObj *drs.DRSObject, dst string) error {
	sumMap := hash.ConvertHashInfoToMap(drsObj.Checksums)
	if len(sumMap) == 0 {
		return fmt.Errorf("no checksums found for DRS object")
	}

	// find sha256 checksum
	var shaSum string
	for csType, cs := range sumMap {
		if csType == hash.ChecksumTypeSHA256.String() {
			shaSum = cs
			break
		}
	}
	if shaSum == "" {
		return fmt.Errorf("no sha256 checksum found for DRS object")
	}

	// create pointer file content
	pointerContent := "version https://git-lfs.github.com/spec/v1\n"
	pointerContent += fmt.Sprintf("oid sha256:%s\n", shaSum)
	pointerContent += fmt.Sprintf("size %d\n", drsObj.Size)

	// write to file
	err := os.WriteFile(dst, []byte(pointerContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write LFS pointer file: %w", err)
	}

	return nil
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
}
