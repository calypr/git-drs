package addref

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	"github.com/calypr/git-drs/internal/resolver"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	"github.com/calypr/syfon/client/hash"
	"github.com/spf13/cobra"
)

var remote string
var remoteType string
var manifestPath string
var dryRun bool
var Cmd = &cobra.Command{
	Use:   "add-ref <drs_uri> <dst path> | --manifest <references.tsv>",
	Short: "Add a reference to an existing DRS object via URI",
	Long:  "Add a reference to an existing DRS object via URI. Requires that the sha256 of the file is already in the cache",
	Args: func(cmd *cobra.Command, args []string) error {
		if manifestPath != "" && len(args) == 0 {
			return nil
		}
		if manifestPath != "" {
			return fmt.Errorf("positional arguments cannot be combined with --manifest")
		}
		return cobra.ExactArgs(2)(cmd, args)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if manifestPath != "" {
			return runManifest(cmd, manifestPath)
		}
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
		selected := cfg.GetRemote(remoteName)
		if selected == nil {
			return fmt.Errorf("remote %q is not configured", remoteName)
		}
		dstPath, err = safeDestination(dstPath)
		if err != nil {
			return err
		}

		client, err := remoteruntime.New(cfg, remoteName, logger)
		if err != nil {
			return err
		}
		if !client.CanResolve() {
			return fmt.Errorf("remote %q cannot resolve DRS objects", remoteName)
		}
		if remoteType != "" && remoteType != string(client.RemoteType) {
			return fmt.Errorf("--remote-type %q conflicts with configured remote %q type %q", remoteType, remoteName, client.RemoteType)
		}

		obj, err := resolveAddRefObject(context.Background(), cfg, remoteName, client, drsUri)
		if err != nil {
			return err
		}
		if dryRun {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n1 reference(s) validated\n", drsUri, args[1], obj.Size)
			return err
		}
		dirPath := filepath.Dir(dstPath)
		_, err = os.Stat(dirPath)
		if os.IsNotExist(err) {
			// The directory does not exist
			os.MkdirAll(dirPath, os.ModePerm)
		}

		oid := addRefLocalOID(drsUri, remoteName, &obj)
		if client.IsReadOnly() {
			if err := lfs.CreateDRSPointer(&obj, dstPath, drsUri); err != nil {
				return err
			}
		} else if hasContentSHA256(&obj) {
			if err := lfs.CreateLfsPointerWithOID(&obj, dstPath, oid); err != nil {
				return err
			}
		} else if err := lfs.CreateDRSPointer(&obj, dstPath, drsUri); err != nil {
			return err
		}
		if _, err := gitrepo.TrackReadOnly(cmd.Context(), args[1]); err != nil {
			return fmt.Errorf("track add-ref destination %s: %w", args[1], err)
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

func safeDestination(dst string) (string, error) {
	if filepath.IsAbs(dst) {
		return "", fmt.Errorf("destination path must be relative to the repository: %s", dst)
	}
	clean := filepath.Clean(dst)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination path escapes the repository: %s", dst)
	}
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}

	// Do not allow an existing path component to redirect the eventual pointer
	// write. Checking only the cleaned path is insufficient: os.WriteFile and
	// MkdirAll follow symlinks, including a symlink at the destination itself.
	current := root
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return "", fmt.Errorf("inspect destination path %s: %w", dst, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("destination path contains a symlink: %s", dst)
		}
	}
	return filepath.Join(root, clean), nil
}

func init() {
	Cmd.Flags().StringVarP(&remote, "remote", "r", "", "target remote DRS server (default: default_remote)")
	Cmd.Flags().StringVar(&remoteType, "remote-type", "", "resolver remote type for DRS references (for example: terra)")
	Cmd.Flags().StringVar(&manifestPath, "manifest", "", "add references from a tab-separated manifest")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and report references without writing pointers")
	_ = Cmd.Flags().MarkHidden("remote-type")
	_ = Cmd.Flags().MarkDeprecated("remote-type", "remote behavior is selected by --remote configuration")
}

type manifestEntry struct {
	uri, path, sha256 string
	size              *int64
	object            drsapi.DrsObject
	destination       string
}

func runManifest(cmd *cobra.Command, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = '\t'
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if len(rows) < 2 {
		return fmt.Errorf("manifest must contain a header and at least one reference")
	}
	columns := map[string]int{}
	for i, name := range rows[0] {
		columns[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, required := range []string{"drs_uri", "path"} {
		if _, ok := columns[required]; !ok {
			return fmt.Errorf("manifest is missing required %q column", required)
		}
	}
	value := func(row []string, name string) string {
		i, ok := columns[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	entries := make([]manifestEntry, 0, len(rows)-1)
	seen := map[string]int{}
	var problems []string
	for i, row := range rows[1:] {
		e := manifestEntry{uri: value(row, "drs_uri"), path: value(row, "path"), sha256: strings.ToLower(strings.TrimPrefix(value(row, "sha256"), "sha256:"))}
		if _, _, err := resolver.NormalizeDRSURI(e.uri); err != nil {
			problems = append(problems, fmt.Sprintf("row %d: %v", i+2, err))
		}
		clean := filepath.Clean(e.path)
		if prior, ok := seen[clean]; ok {
			problems = append(problems, fmt.Sprintf("row %d: duplicate path %q (also row %d)", i+2, e.path, prior))
		} else {
			seen[clean] = i + 2
		}
		e.destination, err = safeDestination(e.path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("row %d: %v", i+2, err))
		}
		if raw := value(row, "size"); raw != "" {
			n, parseErr := strconv.ParseInt(raw, 10, 64)
			if parseErr != nil || n < 0 {
				problems = append(problems, fmt.Sprintf("row %d: invalid size %q", i+2, raw))
			} else {
				e.size = &n
			}
		}
		if e.sha256 != "" && (len(e.sha256) != 64 || strings.Trim(e.sha256, "0123456789abcdef") != "") {
			problems = append(problems, fmt.Sprintf("row %d: invalid sha256", i+2))
		}
		entries = append(entries, e)
	}
	if len(problems) > 0 {
		return fmt.Errorf("manifest validation failed:\n- %s", strings.Join(problems, "\n- "))
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	remoteName, err := cfg.GetRemoteOrDefault(remote)
	if err != nil {
		return err
	}
	runtime, err := remoteruntime.New(cfg, remoteName, drslog.GetLogger())
	if err != nil {
		return err
	}
	for i := range entries {
		obj, resolveErr := resolveAddRefObject(cmd.Context(), cfg, remoteName, runtime, entries[i].uri)
		if resolveErr != nil {
			problems = append(problems, fmt.Sprintf("row %d: %v", i+2, resolveErr))
			continue
		}
		entries[i].object = obj
		if entries[i].size != nil && *entries[i].size != obj.Size {
			problems = append(problems, fmt.Sprintf("row %d: asserted size %d does not match authoritative size %d", i+2, *entries[i].size, obj.Size))
		}
		authSHA := drsobject.NormalizeChecksum(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
		if entries[i].sha256 != "" && !strings.EqualFold(entries[i].sha256, authSHA) {
			problems = append(problems, fmt.Sprintf("row %d: asserted sha256 does not match authoritative checksum", i+2))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("manifest validation failed:\n- %s", strings.Join(problems, "\n- "))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	for _, e := range entries {
		if dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\n", e.uri, e.path, e.object.Size)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(e.destination), 0o755); err != nil {
			return err
		}
		if runtime.RemoteType == config.TerraServerType {
			err = lfs.CreateDRSPointer(&e.object, e.destination, e.uri)
		} else if hasContentSHA256(&e.object) {
			err = lfs.CreateLfsPointerWithOID(&e.object, e.destination, addRefLocalOID(e.uri, remoteName, &e.object))
		} else {
			err = lfs.CreateDRSPointer(&e.object, e.destination, e.uri)
		}
		if err != nil {
			return err
		}
		if _, err := gitrepo.TrackReadOnly(cmd.Context(), e.path); err != nil {
			return fmt.Errorf("track add-ref destination %s: %w", e.path, err)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%d reference(s) %s\n", len(entries), map[bool]string{true: "validated", false: "added"}[dryRun])
	return nil
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

type drsObjectGetter interface {
	GetObject(context.Context, string) (drsapi.DrsObject, error)
}

func resolveAddRefObject(ctx context.Context, cfg *config.Config, primaryRemote config.Remote, primary *remoteruntime.GitContext, drsURI string) (drsapi.DrsObject, error) {
	if primary != nil && primary.RemoteType == config.TerraServerType {
		anvil, err := newAnVILResolver(ctx, primary.Endpoint)
		if err != nil {
			return drsapi.DrsObject{}, err
		}
		resolved, err := anvil.GetObject(ctx, drsURI)
		if err != nil {
			return drsapi.DrsObject{}, err
		}
		checksums := make([]drsapi.Checksum, 0, len(resolved.Checksums))
		for _, checksum := range resolved.Checksums {
			checksums = append(checksums, drsapi.Checksum{Type: checksum.Type, Checksum: checksum.Checksum})
		}
		var name *string
		if resolved.Name != "" {
			name = &resolved.Name
		}
		return drsapi.DrsObject{Id: resolved.ID, Name: name, Size: resolved.Size, SelfUri: resolved.DRSURI, Checksums: checksums}, nil
	}
	objectID, sourceEndpoint, ok := parseDRSURIForSource(drsURI)
	if !ok {
		return primary.Client.DRS().GetObject(ctx, drsURI)
	}
	if sourceRemote, found := configuredRemoteForDRSURIHost(cfg, drsURI); found {
		if sourceRemote == primaryRemote {
			return primary.Client.DRS().GetObject(ctx, objectID)
		}
		client, err := remoteruntime.New(cfg, sourceRemote, primary.Logger)
		if err != nil {
			return drsapi.DrsObject{}, err
		}
		return client.Client.DRS().GetObject(ctx, objectID)
	}
	getter, err := newSourceDRSGetter(sourceEndpoint)
	if err != nil {
		return drsapi.DrsObject{}, err
	}
	return getter.GetObject(ctx, objectID)
}

var newAnVILResolver = func(ctx context.Context, endpoint string) (resolver.Resolver, error) {
	return resolver.NewAnVIL(ctx, endpoint)
}

func parseDRSURIForSource(drsURI string) (objectID string, endpoint string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(drsURI))
	if err != nil || !strings.EqualFold(u.Scheme, "drs") || strings.TrimSpace(u.Host) == "" {
		return "", "", false
	}
	objectID = strings.TrimPrefix(u.EscapedPath(), "/")
	if objectID == "" {
		return "", "", false
	}
	return objectID, "https://" + u.Host, true
}

func configuredRemoteForDRSURIHost(cfg *config.Config, drsURI string) (config.Remote, bool) {
	u, err := url.Parse(strings.TrimSpace(drsURI))
	if err != nil || !strings.EqualFold(u.Scheme, "drs") || strings.TrimSpace(u.Host) == "" {
		return "", false
	}
	for name, selected := range cfg.Remotes {
		remote := config.Config{Remotes: map[config.Remote]config.RemoteSelect{name: selected}}.GetRemote(name)
		if remote == nil {
			continue
		}
		endpoint, err := url.Parse(remote.GetEndpoint())
		if err != nil {
			continue
		}
		if strings.EqualFold(endpoint.Host, u.Host) {
			return name, true
		}
	}
	return "", false
}

var newSourceDRSGetter = newAnonymousSourceDRSGetter

func newAnonymousSourceDRSGetter(endpoint string) (drsObjectGetter, error) {
	raw, err := syclient.New(endpoint)
	if err != nil {
		return nil, err
	}
	client, ok := raw.(*syclient.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected syfon client type %T", raw)
	}
	return client.DRS(), nil
}
