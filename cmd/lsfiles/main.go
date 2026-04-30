package lsfiles

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsremote"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/spf13/cobra"
)

var gitRemote string
var drsRemote string

var (
	loadConfig      = config.LoadConfig
	resolveRemote   = func(cfg *config.Config, name string) (config.Remote, error) { return cfg.GetRemoteOrDefault(name) }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*config.GitContext, error) {
		return cfg.GetRemoteClient(remote, logger)
	}
	loadLFSInventory    = lfs.GetAllLfsFiles
	lookupScopedObjects = drsremote.ObjectsByHashForScope
)

type fileRow struct {
	OID    string
	Status string
	Path   string
	Detail string
}

func collectRows(cmd *cobra.Command, gitRemoteName, drsRemoteName string) ([]fileRow, error) {
	logger := drslog.GetLogger()

	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	remoteName, err := resolveRemote(cfg, drsRemoteName)
	if err != nil {
		logger.Error(fmt.Sprintf("Error getting remote: %v", err))
		return nil, err
	}

	client, err := newRemoteClient(cfg, remoteName, logger)
	if err != nil {
		return nil, err
	}

	lfsFiles, err := loadLFSInventory(gitRemoteName, drsRemoteName, []string{}, logger)
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(lfsFiles))
	for path := range lfsFiles {
		keys = append(keys, path)
	}
	sort.Strings(keys)

	rows := make([]fileRow, 0, len(keys))
	for _, path := range keys {
		info := lfsFiles[path]
		row := fileRow{
			OID:  info.Oid,
			Path: path,
		}

		results, err := lookupScopedObjects(cmd.Context(), client, info.Oid)
		switch {
		case err != nil:
			row.Status = "error"
			row.Detail = err.Error()
		case len(results) == 0:
			row.Status = "missing"
			row.Detail = "-"
		default:
			row.Status = "present"
			ids := make([]string, 0, len(results))
			for _, res := range results {
				ids = append(ids, "drs://"+res.Id)
			}
			row.Detail = strings.Join(ids, ",")
		}

		rows = append(rows, row)
	}

	return rows, nil
}

func printRows(cmd *cobra.Command, rows []fileRow) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "OID\tSTATUS\tPATH\tDETAIL\n"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", row.OID, row.Status, row.Path, row.Detail); err != nil {
			return err
		}
	}
	return nil
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "ls-files",
	Short: "List local LFS-tracked files and their DRS registration status",
	RunE: func(cmd *cobra.Command, args []string) error {
		rows, err := collectRows(cmd, gitRemote, drsRemote)
		if err != nil {
			return err
		}
		return printRows(cmd, rows)
	},
}

func init() {
	Cmd.Flags().StringVarP(&gitRemote, "git-remote", "r", "", "target remote Git server (default: origin)")
	Cmd.Flags().StringVarP(&drsRemote, "drs-remote", "d", "", "target remote DRS server (default: origin)")
}
