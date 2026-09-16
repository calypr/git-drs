package lsfiles

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
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

var (
	loadConfig      = config.LoadConfig
	resolveRemote   = func(cfg *config.Config, name string) (config.Remote, error) { return cfg.GetRemoteOrDefault(name) }
	newRemoteClient = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		return remoteruntime.New(cfg, remote, logger)
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
	lookupScopedObjectsBatch = lookup.ObjectsByHashesForScope
)

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

func collectRows(ctx context.Context, gitRemoteName, drsRemoteName string, patterns []string, resolveDRS bool) ([]fileRow, error) {
	logger := drslog.GetLogger()

	var client *remoteruntime.GitContext
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
			if !matchesAnyPattern(path, patterns) {
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
		drsResults, drsLookupErr = lookupScopedObjectsBatch(ctx, client, oids)
	}
	for _, path := range keys {
		if !matchesAnyPattern(path, patterns) {
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

func shortOID(oid string) string {
	trimmed := strings.TrimSpace(oid)
	if strings.HasPrefix(trimmed, "//") {
		return shortDRSOID(trimmed[2:])
	}
	if len(trimmed) >= len("drs://") && strings.EqualFold(trimmed[:len("drs://")], "drs://") {
		return shortDRSOID(trimmed[len("drs://"):])
	}
	if len(trimmed) <= 10 {
		return trimmed
	}
	return trimmed[:10]
}

func shortDRSOID(id string) string {
	i := strings.IndexByte(id, '/')
	if i < 0 {
		i = strings.IndexByte(id, ':')
	}
	if i >= 0 {
		end := i + 1 + 11
		if end > len(id) {
			end = len(id)
		}
		return id[:end]
	}
	if len(id) > 18 {
		return id[:18]
	}
	return id
}

func matchesAnyPattern(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchesPattern(normalized, pattern) {
			return true
		}
	}
	return false
}

func matchesPattern(path, pattern string) bool {
	pattern = filepath.ToSlash(filepath.Clean(pattern))
	if !strings.ContainsAny(pattern, "*?[") {
		return path == pattern
	}
	re, err := regexp.Compile(globToRegexp(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(path)
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
				continue
			}
			b.WriteString(`[^/]*`)
		case '?':
			b.WriteString(`[^/]`)
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}

func isLocalized(path string) bool {
	payload, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, _, ok := lfs.ParseLFSPointer(payload)
	return !ok
}
