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

type globusEntry struct {
	rel, source, destination, oid, sha256 string
	size                                  int64
}

type globusLister interface {
	ListFiles(context.Context, string, string) ([]globusauth.File, error)
	StatFile(context.Context, string, string) (globusauth.File, error)
	Close() error
}

var newGlobusLister = func(ctx context.Context) (globusLister, error) { return globusauth.NewClient(ctx) }

type globusSource struct {
	collection, root, pattern string
	tree                      bool
}

func (s *AddURLService) runGlobus(ctx context.Context, cmd *cobra.Command, logger *slog.Logger, input addURLInput) error {
	source, err := parseGlobusSource(input.sourceArg)
	if err != nil {
		return err
	}
	client, err := newGlobusLister(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	source, files, err := discoverGlobusFiles(ctx, client, source)
	if err != nil {
		return err
	}
	if source.tree && !input.explicitPath {
		return fmt.Errorf("Globus directories and wildcards require an explicit destination directory")
	}
	if source.tree && input.sha256 != "" {
		return fmt.Errorf("--sha256 can only describe one Globus file")
	}

	var entries []globusEntry
	if input.manifest != "" {
		if !source.tree {
			return fmt.Errorf("--manifest is only supported for Globus directories and wildcards")
		}
		entries, err = readGlobusManifest(input.manifest, source.collection, source.root, input.path)
		if err == nil {
			err = compareGlobusManifest(entries, files)
		}
	} else {
		entries, err = globusEntries(files, source, input.path, input.sha256)
	}
	if err != nil {
		return err
	}
	if err := validateImportDestinations(entries); err != nil {
		return err
	}
	for _, entry := range entries {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n", entry.source, entry.destination, entry.size)
	}
	if input.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "%d object(s) validated\n", len(entries))
		return nil
	}
	trackPattern := input.path
	if source.tree {
		if err := ensureReadOnlyTree(input.path); err != nil {
			return err
		}
		trackPattern = filepath.ToSlash(filepath.Join(input.path, "**"))
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
		if err := writePointerFile(entry.destination, entry.oid, entry.size, entry.sha256 == ""); err != nil {
			return err
		}
		if err := updatePrecommitCache(ctx, logger, entry.destination, entry.oid, entry.source); err != nil {
			logger.Warn("pre-commit cache update skipped", "path", entry.destination, "error", err)
		}
		if _, err := writeAddURLDrsObject(builder, addURLDrsFile{Name: entry.destination, Size: entry.size, Oid: entry.oid, ContentSHA256: entry.sha256}, entry.source); err != nil {
			return err
		}
	}
	if _, err := gitrepo.TrackReadOnly(ctx, trackPattern); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%d object(s) added; stage %s and .gitattributes, then run git drs push\n", len(entries), input.path)
	return nil
}

func parseGlobusSource(raw string) (globusSource, error) {
	parseRaw := raw
	if schemeEnd := strings.Index(parseRaw, "://"); schemeEnd >= 0 {
		if pathStart := strings.Index(parseRaw[schemeEnd+3:], "/"); pathStart >= 0 {
			pathStart += schemeEnd + 3
			parseRaw = parseRaw[:pathStart] + strings.ReplaceAll(parseRaw[pathStart:], "?", "%3F")
		}
	}
	u, err := url.Parse(parseRaw)
	if err != nil || !strings.EqualFold(u.Scheme, "globus") || u.Host == "" || u.Path == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return globusSource{}, fmt.Errorf("invalid Globus URL %q; expected globus://<collection-id>/<path>", raw)
	}
	clean := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	source := globusSource{collection: u.Host, root: clean}
	if strings.ContainsAny(clean, "*?[") {
		if _, err := matchGlobusPath(clean, clean); err != nil {
			return globusSource{}, fmt.Errorf("invalid Globus wildcard %q: %w", clean, err)
		}
		source.root = globusPatternRoot(clean)
		source.pattern = clean
		source.tree = true
	} else if strings.HasSuffix(u.Path, "/") {
		source.tree = true
	}
	return source, nil
}

func discoverGlobusFiles(ctx context.Context, client globusLister, source globusSource) (globusSource, []globusauth.File, error) {
	if !source.tree {
		file, err := client.StatFile(ctx, source.collection, source.root)
		if err != nil {
			return source, nil, err
		}
		if !file.Directory {
			return source, []globusauth.File{file}, nil
		}
		source.tree = true
	}
	files, err := client.ListFiles(ctx, source.collection, source.root)
	if err != nil {
		return source, nil, err
	}
	if source.pattern != "" {
		matched := files[:0]
		for _, file := range files {
			ok, err := matchGlobusPath(source.pattern, file.Path)
			if err != nil {
				return source, nil, fmt.Errorf("invalid Globus wildcard %q: %w", source.pattern, err)
			}
			if ok {
				matched = append(matched, file)
			}
		}
		files = matched
	}
	if len(files) == 0 {
		return source, nil, fmt.Errorf("Globus URL matched no files")
	}
	return source, files, nil
}

func globusPatternRoot(pattern string) string {
	var parts []string
	for _, part := range strings.Split(strings.TrimPrefix(pattern, "/"), "/") {
		if strings.ContainsAny(part, "*?[") {
			break
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "/"
	}
	return "/" + path.Join(parts...)
}

func matchGlobusPath(pattern, name string) (bool, error) {
	patternParts := strings.Split(strings.TrimPrefix(path.Clean(pattern), "/"), "/")
	nameParts := strings.Split(strings.TrimPrefix(path.Clean(name), "/"), "/")
	var match func(int, int) (bool, error)
	// ponytail: recursion is bounded by URL path depth; memoize only if deep
	// patterns containing multiple ** segments become a measured bottleneck.
	match = func(pi, ni int) (bool, error) {
		if pi == len(patternParts) {
			return ni == len(nameParts), nil
		}
		if patternParts[pi] == "**" {
			if ok, err := match(pi+1, ni); ok || err != nil {
				return ok, err
			}
			if ni < len(nameParts) {
				return match(pi, ni+1)
			}
			return false, nil
		}
		if ni == len(nameParts) {
			return false, nil
		}
		ok, err := path.Match(patternParts[pi], nameParts[ni])
		if err != nil || !ok {
			return ok, err
		}
		return match(pi+1, ni+1)
	}
	return match(0, 0)
}

func globusEntries(files []globusauth.File, source globusSource, destination, sha256 string) ([]globusEntry, error) {
	if sha256 != "" {
		var err error
		sha256, err = normalizeSHA256(sha256)
		if err != nil {
			return nil, err
		}
	}
	entries := make([]globusEntry, 0, len(files))
	for _, file := range files {
		rel := path.Base(file.Path)
		entryDestination := destination
		if source.tree {
			rel = strings.TrimPrefix(file.Path, strings.TrimRight(source.root, "/")+"/")
			if rel == file.Path || rel == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
				return nil, fmt.Errorf("invalid Globus member path %q", file.Path)
			}
			entryDestination = filepath.ToSlash(filepath.Join(destination, filepath.FromSlash(rel)))
		}
		sourceURL := (&url.URL{Scheme: "globus", Host: source.collection, Path: file.Path}).String()
		oid := sha256
		if oid == "" {
			modified := strings.TrimSpace(file.LastModified)
			if modified == "" {
				hint := "supply --sha256"
				if source.tree {
					hint = "supply --manifest with SHA-256 checksums"
				}
				return nil, fmt.Errorf("Globus did not report last_modified for %s; %s", sourceURL, hint)
			}
			var err error
			// ponytail: Globus exposes no content checksum here; callers that need
			// stronger change detection can supply an authoritative manifest.
			oid, err = placeholderOIDForUnknownSHA("last_modified="+modified+";size="+strconv.FormatInt(file.Size, 10), sourceURL)
			if err != nil {
				return nil, err
			}
		}
		entries = append(entries, globusEntry{rel: rel, source: sourceURL, destination: entryDestination, size: file.Size, oid: oid, sha256: sha256})
	}
	return entries, nil
}

func normalizeSHA256(raw string) (string, error) {
	sha := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "sha256:"))
	if len(sha) != 64 || strings.Trim(sha, "0123456789abcdef") != "" {
		return "", fmt.Errorf("invalid SHA-256 %q; expected 64 hexadecimal characters", raw)
	}
	return sha, nil
}

func readGlobusManifest(filename, collection, sourceRoot, destinationRoot string) ([]globusEntry, error) {
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
	var entries []globusEntry
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
		sha, err := normalizeSHA256(value("sha256"))
		if err != nil {
			return nil, fmt.Errorf("manifest row %d has invalid sha256", i+2)
		}
		if seenSHA[sha] {
			return nil, fmt.Errorf("manifest row %d repeats sha256 %s; one-member-per-object import requires unique content", i+2, sha)
		}
		seenSHA[sha] = true
		sourcePath := path.Join(sourceRoot, rel)
		entries = append(entries, globusEntry{rel: rel, source: (&url.URL{Scheme: "globus", Host: collection, Path: sourcePath}).String(), destination: filepath.ToSlash(filepath.Join(destinationRoot, filepath.FromSlash(rel))), size: size, oid: sha, sha256: sha})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	return entries, nil
}

func compareGlobusManifest(entries []globusEntry, files []globusauth.File) error {
	if len(entries) != len(files) {
		return fmt.Errorf("Globus selection contains %d files but manifest contains %d", len(files), len(entries))
	}
	for i := range entries {
		u, _ := url.Parse(entries[i].source)
		if files[i].Path != u.Path || files[i].Size != entries[i].size {
			return fmt.Errorf("Globus collection does not match manifest at %q", entries[i].rel)
		}
	}
	return nil
}

func validateImportDestinations(entries []globusEntry) error {
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
