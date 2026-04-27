package lsfiles

import (
	"fmt"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drslookup"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/spf13/cobra"
)

var gitRemote string
var drsRemote string

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "ls-files",
	Short: "List files in project and print status",
	RunE: func(cmd *cobra.Command, args []string) error {

		logger := drslog.GetLogger()

		config, err := config.LoadConfig()
		if err != nil {
			return err
		}

		remoteName, err := config.GetRemoteOrDefault(drsRemote)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return err
		}

		client, err := config.GetRemoteClient(remoteName, logger)
		if err != nil {
			return err
		}

		_ = client //debug: print client for now, will implement actual listing logic later

		lfsFiles, err := lfs.GetAllLfsFiles(gitRemote, drsRemote, []string{}, logger)
		if err != nil {
			return err
		}

		for fileName, info := range lfsFiles {

			results, err := drslookup.ObjectsByHashForScope(cmd.Context(), client, info.Oid)
			if err != nil {
				fmt.Printf("%s x %s\n", info.Oid, fileName)
			} else {
				ids := []string{}
				for _, res := range results {
					ids = append(ids, "drs://"+res.Id)
				}
				idstr := strings.Join(ids, ",")
				fmt.Printf("%s + %s\t%s\n", info.Oid, fileName, idstr)
			}
		}

		return nil
	},
}

func init() {
	Cmd.Flags().StringVarP(&gitRemote, "git-remote", "r", "", "target remote Git server (default: origin)")
	Cmd.Flags().StringVarP(&drsRemote, "drs-remote", "d", "", "target remote DRS server (default: origin)")
}
