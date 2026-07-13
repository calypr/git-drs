package addref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var remote string
var remoteType string
var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <dst path>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object via URI. Requires that the sha256 of the file is already in the cache",
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

		client, err := remoteruntime.New(cfg, remoteName, logger)
		if err != nil {
			return err
		}

		obj, err := client.Client.DRS().GetObject(context.Background(), drsUri)
		if err != nil {
			return err
		}
		dirPath := filepath.Dir(dstPath)
		_, err = os.Stat(dirPath)
		if os.IsNotExist(err) {
			// The directory does not exist
			os.MkdirAll(dirPath, os.ModePerm)
		}

		oid := addRefLocalOID(drsUri, remoteName, &obj)
		if hasContentSHA256(&obj) {
			if err := lfs.CreateLfsPointerWithOID(&obj, dstPath, oid); err != nil {
				return err
			}
		} else if err := lfs.CreateDRSPointer(&obj, dstPath, drsUri); err != nil {
			return err
		}
		if obj.SelfUri == "" {
			obj.SelfUri = drsUri
		}
		if err := drsobject.WriteObject(gitrepo.DRSObjectsPath, &obj, oid); err != nil {
			return fmt.Errorf("write source DRS metadata: %w", err)
		}
		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVar(&remoteType, "remote-type", "", "resolver remote type for DRS references (for example: terra)")
}

func hasContentSHA256(obj *drsapi.DrsObject) bool {
	return obj != nil && drsobject.NormalizeChecksum(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256) != ""
}

func addRefLocalOID(sourceURI string, remoteName config.Remote, obj *drsapi.DrsObject) string {
	if obj != nil {
		if sha := drsobject.NormalizeChecksum(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256); sha != "" {
			return sha
		}
	}
	return derivedSourceOID(sourceURI, string(remoteName))
}

func derivedSourceOID(sourceURI, remoteName string) string {
	sum := sha256.Sum256([]byte("git-drs-source-ref:v1\nsource_uri=" + sourceURI + "\nremote=" + remoteName + "\n"))
	return hex.EncodeToString(sum[:])
}
