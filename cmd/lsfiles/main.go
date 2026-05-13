package lsfiles

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsremote"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/pathspec"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/spf13/cobra"
)

var gitRemote string
var drsRemote string
var includePatterns []string
var showLong bool
var nameOnly bool
var jsonOutput bool
var drsStatus bool

var (
	loadConfig      = config.LoadConfig
	resolveRemote   = func(cfg *config.Config, name string) (config.Remote, error) { return cfg.GetRemoteOrDefault(name) }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*config.GitContext, error) {
		return cfg.GetRemoteClient(remote, logger)
	}
	loadLFSInventory = func(gitRemoteName, gitRemoteLocation string, branches []string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		if len(branches) == 0 {
			return lfs.GetTrackedLfsFiles(logger)
		}
		return lfs.GetLfsFilesForRefs(branches, logger)
	}
	listRemoteRefs           = defaultListRemoteRefs
	listGitRemotes           = defaultListGitRemotes
	resolveDefaultRemote     = defaultResolveDefaultRemote
	lookupScopedObjectsBatch = drsremote.ObjectsByHashesForScope
)

type fileRow struct {
	OID        string   `json:"oid"`
	ShortOID   string   `json:"short_oid"`
	Status     string   `json:"status"`
	Path       string   `json:"path"`
	Localized  bool     `json:"localized"`
	Registered bool     `json:"registered,omitempty"`
	DRSIDs     []string `json:"drs_ids,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}

func defaultListRemoteRefs(gitRemoteName string) ([]string, error) {
	if strings.TrimSpace(gitRemoteName) == "" {
		return nil, nil
	}

	cmd := exec.Command("git", "for-each-ref", "--format=%(refname)", "refs/remotes/"+gitRemoteName)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list refs for remote %s: %w", gitRemoteName, err)
	}

	lines := strings.Split(string(out), "\n")
	refs := make([]string, 0, len(lines))
	for _, line := range lines {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs, nil
}

func defaultListGitRemotes() ([]string, error) {
	cmd := exec.Command("git", "remote")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list git remotes: %w", err)
	}

	lines := strings.Split(string(out), "\n")
	remotes := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		remotes = append(remotes, name)
	}
	sort.Strings(remotes)
	return remotes, nil
}

func defaultResolveDefaultRemote() string {
	cfg, err := loadConfig()
	if err == nil && cfg != nil {
		if remote, err := cfg.GetRemoteOrDefault(""); err == nil {
			return strings.TrimSpace(string(remote))
		}
	}

	remotes, err := listGitRemotes()
	if err != nil || len(remotes) == 0 {
		return ""
	}
	for _, remote := range remotes {
		if remote == config.ORIGIN {
			return remote
		}
	}
	if len(remotes) == 1 {
		return remotes[0]
	}
	return ""
}

func collectRows(cmd *cobra.Command, gitRemoteName, drsRemoteName string, patterns []string, resolveDRS bool) ([]fileRow, error) {
	logger := drslog.GetLogger()

	var client *config.GitContext
	if resolveDRS {
		cfg, err := loadConfig()
		if err != nil {
			return nil, err
		}

		remoteName, err := resolveRemote(cfg, drsRemoteName)
		if err != nil {
			logger.Error(fmt.Sprintf("Error getting remote: %v", err))
			return nil, err
		}

		client, err = newRemoteClient(cfg, remoteName, logger)
		if err != nil {
			return nil, err
		}
	}

	var (
		refs []string
		err  error
	)
	if strings.TrimSpace(gitRemoteName) != "" {
		refs, err = listRemoteRefs(gitRemoteName)
		if err != nil {
			return nil, err
		}
	}

	lfsFiles, err := loadLFSInventory(gitRemoteName, drsRemoteName, refs, logger)
	if err != nil {
		return nil, err
	}
	if len(lfsFiles) == 0 && strings.TrimSpace(gitRemoteName) == "" {
		fallbackRemote := resolveDefaultRemote()
		if fallbackRemote != "" {
			refs, err = listRemoteRefs(fallbackRemote)
			if err != nil {
				return nil, err
			}
			if len(refs) > 0 {
				lfsFiles, err = loadLFSInventory(fallbackRemote, drsRemoteName, refs, logger)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	keys := make([]string, 0, len(lfsFiles))
	for path := range lfsFiles {
		keys = append(keys, path)
	}
	sort.Strings(keys)

	rows := make([]fileRow, 0, len(keys))
	var drsResults map[string][]drsapi.DrsObject
	var drsLookupErr error
	if resolveDRS {
		oids := make([]string, 0, len(keys))
		seenOIDs := make(map[string]struct{}, len(keys))
		for _, path := range keys {
			if !pathspec.MatchesAny(path, patterns) {
				continue
			}
			oid := lfsFiles[path].Oid
			if oid == "" {
				continue
			}
			if _, exists := seenOIDs[oid]; exists {
				continue
			}
			seenOIDs[oid] = struct{}{}
			oids = append(oids, oid)
		}
		drsResults, drsLookupErr = lookupScopedObjectsBatch(cmd.Context(), client, oids)
	}
	for _, path := range keys {
		if !pathspec.MatchesAny(path, patterns) {
			continue
		}
		info := lfsFiles[path]
		row := fileRow{
			OID:       info.Oid,
			ShortOID:  shortOID(info.Oid),
			Path:      path,
			Localized: isLocalized(path),
		}
		row.Status = "-"
		if row.Localized {
			row.Status = "*"
		}

		if resolveDRS {
			switch {
			case drsLookupErr != nil:
				row.Detail = drsLookupErr.Error()
			default:
				results := drsResults[info.Oid]
				if len(results) == 0 {
					row.Registered = false
					break
				}
				row.Registered = true
				row.DRSIDs = make([]string, 0, len(results))
				for _, res := range results {
					row.DRSIDs = append(row.DRSIDs, "drs://"+res.Id)
				}
				row.Detail = strings.Join(row.DRSIDs, ",")
			}
		}

		rows = append(rows, row)
	}

	return rows, nil
}

func printRows(cmd *cobra.Command, rows []fileRow) error {
	if jsonOutput {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	for _, row := range rows {
		switch {
		case nameOnly:
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), row.Path); err != nil {
				return err
			}
		case drsStatus:
			oid := row.ShortOID
			if showLong {
				oid = row.OID
			}
			detail := row.Detail
			if detail == "" {
				detail = "-"
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\t%s\n", oid, row.Status, row.Path, detail); err != nil {
				return err
			}
		default:
			oid := row.ShortOID
			if showLong {
				oid = row.OID
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s\n", oid, row.Status, row.Path); err != nil {
				return err
			}
		}
	}
	return nil
}

func shortOID(oid string) string {
	if len(oid) <= 10 {
		return oid
	}
	return oid[:10]
}

func isLocalized(path string) bool {
	payload, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, _, ok := lfs.ParseLFSPointer(payload)
	return !ok
}

func validateOutputFlags() error {
	if nameOnly && jsonOutput {
		return fmt.Errorf("--name-only and --json are mutually exclusive")
	}
	if showLong && nameOnly {
		return fmt.Errorf("--long and --name-only are mutually exclusive")
	}
	return nil
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "ls-files [pathspec...]",
	Short: "List tracked DRS/LFS pointer files in the repository",
	Long:  "List tracked DRS/Git-LFS pointer files in the repository. By default this behaves like a local file inventory. Use --drs to also resolve DRS registration status.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutputFlags(); err != nil {
			return err
		}
		patterns := append([]string{}, includePatterns...)
		patterns = append(patterns, args...)
		rows, err := collectRows(cmd, gitRemote, drsRemote, patterns, drsStatus)
		if err != nil {
			return err
		}
		return printRows(cmd, rows)
	},
}

func init() {
	Cmd.Flags().StringVarP(&gitRemote, "git-remote", "r", "", "target remote Git server (default: origin)")
	Cmd.Flags().StringVarP(&drsRemote, "drs-remote", "d", "", "target remote DRS server (default: origin)")
	Cmd.Flags().StringArrayVarP(&includePatterns, "include", "I", nil, "include pathspec/glob pattern(s)")
	Cmd.Flags().BoolVarP(&showLong, "long", "l", false, "show full object IDs")
	Cmd.Flags().BoolVarP(&nameOnly, "name-only", "n", false, "show only file paths")
	Cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output")
	Cmd.Flags().BoolVar(&drsStatus, "drs", false, "include DRS registration lookup details")
}
