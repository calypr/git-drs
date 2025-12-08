package push

import (
	"os"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "push",
	Short: "push local objects to drs server.",
	Long:  "push local objects to drs server. Any local files that do not have drs records are written to a bucket.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var remote config.Remote = config.ORIGIN
		if len(args) > 0 {
			remote = config.Remote(args[0])
		}

		myLogger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Printf("Error loading config: %v", err)
			return err
		}

		drsClient, err := cfg.GetRemoteClient(remote, myLogger)
		if err != nil {
			myLogger.Printf("Error creating indexd client: %s", err)
			return err
		}

		// Gather all objects in .drs/lfs/objects store
		drsLfsObjs, err := drs.GetDrsLfsObjects(myLogger)
		if err != nil {
			return err
		}

		// Make this a map if it does not exist when hitting the server
		sums := make([]*hash.Checksum, 0)
		for _, obj := range drsLfsObjs {
			for sumType, sum := range hash.ConvertHashInfoToMap(obj.Checksums) {
				if sumType == hash.ChecksumTypeSHA256.String() {
					sums = append(sums, &hash.Checksum{
						Checksum: sum,
						Type:     hash.ChecksumTypeSHA256,
					})
				}
			}
		}

		drsMap, err := drsClient.GetSha256ObjMap(sums...)
		if err != nil {
			return err
		}

		for drsObjKey, _ := range drsMap {
			val, ok := drsLfsObjs[drsObjKey]
			if !ok {
				myLogger.Printf("Drs record not found in sha256 map %s", drsObjKey)
			}
			if _, statErr := os.Stat(val.Name); os.IsNotExist(statErr) {
				myLogger.Printf("Error: Object record found locally, but file does not exist locally. Registering Record %s", val.Name)
				drsClient.RegisterRecord(val)

			} else {
				drsClient.RegisterFile(drsObjKey)
			}
		}
		return nil
	},
}
