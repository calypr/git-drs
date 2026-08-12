package addurl

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/spf13/cobra"
)

type globusManifestEntry struct {
	rel, source, destination, sha256 string
	size                             int64
}

type globusLister interface {
	ListFiles(context.Context, string, string) ([]globusauth.File, error)
	Close() error
}

var newGlobusLister = func(ctx context.Context) (globusLister, error) { return globusauth.NewClient(ctx) }

func (s *AddURLService) runRecursiveGlobus(ctx context.Context, cmd *cobra.Command, logger *slog.Logger, input addURLInput) error {
	if input.manifest == "" {
		return fmt.Errorf("recursive Globus import requires --manifest with path, size, and sha256 columns")
	}
	u, err := url.Parse(input.sourceArg)
	if err != nil || !strings.EqualFold(u.Scheme, "globus") || u.Host == "" || u.Path == "" {
		return fmt.Errorf("--recursive requires globus://<collection-id>/<source-root>/")
	}
	if input.path == "" || input.path == strings.TrimPrefix(u.Path, "/") {
		return fmt.Errorf("recursive Globus import requires an explicit destination directory")
	}
	root := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	entries, err := readGlobusManifest(input.manifest, u.Host, root, input.path)
	if err != nil {
		return err
	}
	if err := validateImportDestinations(entries); err != nil {
		return err
	}
	client, err := newGlobusLister(ctx)
	if err != nil {
		return err
	}
	files, err := client.ListFiles(ctx, u.Host, root)
	client.Close()
	if err != nil {
		return err
	}
	if err := compareGlobusManifest(entries, files); err != nil {
		return err
	}
	for _, entry := range entries {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n", entry.source, entry.destination, entry.size)
	}
	if input.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "%d object(s) validated\n", len(entries))
		return nil
	}
	if err := ensureReadOnlyTree(input.path); err != nil {
		return err
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return err
	}
	remote, err := cfg.GetRemoteOrDefault(input.remote)
	if err != nil {
		return err
	}
	remoteConfig := cfg.GetRemote(remote)
	if remoteConfig == nil {
		return fmt.Errorf("remote %q is not configured", remote)
	}
	org, project, scope, err := resolveTargetScope(remoteConfig)
	if err != nil {
		return err
	}
	builder := drsobjectBuilder(scope.Bucket, org, project, scope.Prefix)
	for _, entry := range entries {
		if err := writePointerFile(entry.destination, entry.sha256, entry.size); err != nil {
			return err
		}
		if err := updatePrecommitCache(ctx, logger, entry.destination, entry.sha256, entry.source); err != nil {
			logger.Warn("pre-commit cache update skipped", "path", entry.destination, "error", err)
		}
		if _, err := writeAddURLDrsObject(builder, addURLDrsFile{Name: entry.destination, Size: entry.size, Oid: entry.sha256, ContentSHA256: entry.sha256}, entry.source); err != nil {
			return err
		}
	}
	if _, err := gitrepo.TrackReadOnly(ctx, filepath.ToSlash(filepath.Join(input.path, "**"))); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%d object(s) added; stage %s and .gitattributes, then run git drs push\n", len(entries), input.path)
	return nil
}

func readGlobusManifest(filename, collection, sourceRoot, destinationRoot string) ([]globusManifestEntry, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open Globus manifest: %w", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = '\t'
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, fmt.Errorf("Globus manifest must contain a header and at least one row")
	}
	columns := map[string]int{}
	for i, column := range rows[0] {
		columns[strings.ToLower(strings.TrimSpace(column))] = i
	}
	for _, required := range []string{"path", "size", "sha256"} {
		if _, ok := columns[required]; !ok {
			return nil, fmt.Errorf("Globus manifest is missing required %q column", required)
		}
	}
	var entries []globusManifestEntry
	seen := map[string]bool{}
	seenSHA := map[string]bool{}
	for i, row := range rows[1:] {
		value := func(name string) string {
			idx := columns[name]
			if idx >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[idx])
		}
		rel := path.Clean(strings.TrimPrefix(value("path"), "/"))
		if rel == "." || !filepath.IsLocal(filepath.FromSlash(rel)) || seen[rel] {
			return nil, fmt.Errorf("manifest row %d has invalid or duplicate path %q", i+2, value("path"))
		}
		seen[rel] = true
		size, err := strconv.ParseInt(value("size"), 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("manifest row %d has invalid size", i+2)
		}
		sha := strings.ToLower(strings.TrimPrefix(value("sha256"), "sha256:"))
		if len(sha) != 64 || strings.Trim(sha, "0123456789abcdef") != "" {
			return nil, fmt.Errorf("manifest row %d has invalid sha256", i+2)
		}
		if seenSHA[sha] {
			return nil, fmt.Errorf("manifest row %d repeats sha256 %s; one-member-per-object import requires unique content", i+2, sha)
		}
		seenSHA[sha] = true
		sourcePath := path.Join(sourceRoot, rel)
		entries = append(entries, globusManifestEntry{rel: rel, source: (&url.URL{Scheme: "globus", Host: collection, Path: sourcePath}).String(), destination: filepath.ToSlash(filepath.Join(destinationRoot, filepath.FromSlash(rel))), size: size, sha256: sha})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	return entries, nil
}

func compareGlobusManifest(entries []globusManifestEntry, files []globusauth.File) error {
	if len(entries) != len(files) {
		return fmt.Errorf("Globus collection contains %d files but manifest contains %d", len(files), len(entries))
	}
	for i := range entries {
		u, _ := url.Parse(entries[i].source)
		if files[i].Path != u.Path || files[i].Size != entries[i].size {
			return fmt.Errorf("Globus collection does not match manifest at %q", entries[i].rel)
		}
	}
	return nil
}

func validateImportDestinations(entries []globusManifestEntry) error {
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !filepath.IsLocal(filepath.FromSlash(entry.destination)) {
			return fmt.Errorf("destination escapes repository: %s", entry.destination)
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(entry.destination))); err == nil {
			return fmt.Errorf("destination already exists: %s", entry.destination)
		} else if !os.IsNotExist(err) {
			return err
		}
		current := root
		for _, component := range strings.Split(filepath.FromSlash(entry.destination), string(filepath.Separator)) {
			current = filepath.Join(current, component)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("destination contains symlink: %s", entry.destination)
			}
		}
	}
	return nil
}

func ensureReadOnlyTree(destination string) error {
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	pattern := filepath.ToSlash(filepath.Join(destination, "**"))
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == pattern {
			for _, field := range fields[1:] {
				if field == "drs=rw" || field == "drs.route=rw" {
					return fmt.Errorf("%s is already configured drs=rw in .gitattributes", pattern)
				}
			}
		}
	}
	return nil
}
