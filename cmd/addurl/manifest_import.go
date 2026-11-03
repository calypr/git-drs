package addurl

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/cmd/copyrecords"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	"github.com/calypr/syfon/client/apierror"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type manifestEntry struct {
	source, destination, sha string
	size                     int64
	resolved                 string
}

func readImportManifest(filename string) ([]manifestEntry, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read manifest header: %w", err)
	}
	cols := map[string]int{}
	for i, v := range header {
		key := strings.ToLower(strings.TrimSpace(v))
		if _, ok := cols[key]; ok {
			return nil, fmt.Errorf("duplicate column %q", key)
		}
		cols[key] = i
	}
	for _, key := range []string{"s3_path", "repo_path", "sha256", "size_bytes"} {
		if _, ok := cols[key]; !ok {
			return nil, fmt.Errorf("manifest missing %q column", key)
		}
	}
	var entries []manifestEntry
	paths := map[string]bool{}
	hashes := map[string]bool{}
	for line := 2; ; line++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("manifest row %d: %w", line, err)
		}
		val := func(key string) string {
			i := cols[key]
			if i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		dest, source := val("repo_path"), val("s3_path")
		if dest == "" || !filepath.IsLocal(filepath.FromSlash(dest)) || path.Clean(dest) != dest || strings.ContainsAny(dest, "\\\x00\n\r") || strings.HasPrefix(dest, ".git/") || dest == ".git" || paths[dest] {
			return nil, fmt.Errorf("manifest row %d: invalid or duplicate repo_path %q", line, dest)
		}
		paths[dest] = true
		if source == "" {
			return nil, fmt.Errorf("manifest row %d: missing s3_path", line)
		}
		sha, err := normalizeSHA256(val("sha256"))
		if err != nil {
			return nil, fmt.Errorf("manifest row %d: %w", line, err)
		}
		if hashes[sha] {
			return nil, fmt.Errorf("manifest row %d: repeated SHA-256 %s", line, sha)
		}
		hashes[sha] = true
		size, err := strconv.ParseInt(val("size_bytes"), 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("manifest row %d: invalid size_bytes", line)
		}
		entries = append(entries, manifestEntry{source: source, destination: dest, sha: sha, size: size})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("manifest contains no entries")
	}
	for _, e := range entries {
		for parent := path.Dir(e.destination); parent != "."; parent = path.Dir(parent) {
			if paths[parent] {
				return nil, fmt.Errorf("manifest path %q is nested under another manifest file %q", e.destination, parent)
			}
		}
	}
	return entries, nil
}

func probeManifest(ctx context.Context, api *internalapi.ClientWithResponses, entries []manifestEntry, org, project, strip string) error {
	for start := 0; start < len(entries); start += 100 {
		end := min(start+100, len(entries))
		items := make([]internalapi.InternalInspectObjectRequest, 0, end-start)
		for i := start; i < end; i++ {
			e := entries[i]
			request := internalapi.InternalInspectObjectRequest{Id: strconv.Itoa(i), ExpectedSizeBytes: &e.size}
			source := e.source
			if strip != "" {
				if !strings.HasPrefix(source, strip) {
					return fmt.Errorf("manifest row %d: source does not start with --strip-prefix", i+2)
				}
				source = strings.TrimPrefix(source, strip)
			}
			if strings.HasPrefix(strings.ToLower(source), "s3://") {
				u, err := url.Parse(source)
				if err != nil || u.Host == "" || u.Path == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
					return fmt.Errorf("manifest row %d: invalid S3 URL", i+2)
				}
				request.ObjectUrl = source
			} else {
				if strings.Contains(source, "://") || !filepath.IsLocal(filepath.FromSlash(source)) {
					return fmt.Errorf("manifest row %d: invalid scoped key", i+2)
				}
				request.Organization = org
				request.Project = project
				request.Scheme = "s3"
				request.Key = source
			}
			items = append(items, request)
		}
		resp, err := api.InternalInspectObjectBulkWithResponse(ctx, internalapi.InternalInspectObjectsBulkRequest{Items: items})
		if err != nil {
			return err
		}
		if resp.JSON200 == nil {
			return apierror.FromResponse(resp.HTTPResponse, resp.Body)
		}
		if len(resp.JSON200.Items) != len(items) {
			return fmt.Errorf("bulk probe returned %d results for %d objects", len(resp.JSON200.Items), len(items))
		}
		seen := make(map[string]bool, len(items))
		for _, result := range resp.JSON200.Items {
			n, err := strconv.Atoi(result.Id)
			if err != nil || n < start || n >= end || seen[result.Id] {
				return fmt.Errorf("bulk probe returned invalid result id %q", result.Id)
			}
			seen[result.Id] = true
			if result.Status != "present" || !result.Exists || result.SizeMatch == nil || !*result.SizeMatch || result.ObjectUrl == "" {
				return fmt.Errorf("probe %s: status=%s error=%s size_match=%v", entries[n].source, result.Status, result.Error, result.SizeMatch)
			}
			entries[n].resolved = result.ObjectUrl
		}
	}
	return nil
}

func runManifest(cmd *cobra.Command, args []string) (retErr error) {
	for _, flag := range []string{"sha256", "scheme", "recursive", "manifest"} {
		if cmd.Flags().Changed(flag) {
			return fmt.Errorf("--%s is no longer supported; use a TSV manifest", flag)
		}
	}
	repo, _ := cmd.Flags().GetString("repo")
	branch, _ := cmd.Flags().GetString("branch")
	commit, _ := cmd.Flags().GetBool("commit")
	gitOnly, _ := cmd.Flags().GetBool("git-only")
	commit = commit || gitOnly
	remote, _ := cmd.Flags().GetString("remote")
	strip, _ := cmd.Flags().GetString("strip-prefix")
	dry, _ := cmd.Flags().GetBool("dry-run")
	if strings.TrimSpace(repo) == "" {
		return fmt.Errorf("--repo cannot be empty")
	}
	if commit && strings.TrimSpace(branch) == "" {
		return fmt.Errorf("--branch is required with --commit")
	}
	if !commit && branch != "" {
		return fmt.Errorf("--branch requires --commit")
	}
	manifest, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	entries, err := readImportManifest(manifest)
	if err != nil {
		return err
	}
	repo, err = filepath.Abs(repo)
	if err != nil {
		return err
	}
	old, err := os.Getwd()
	if err != nil {
		return err
	}
	if err = os.Chdir(repo); err != nil {
		return err
	}
	defer os.Chdir(old)
	run := func(args ...string) (string, error) {
		c := exec.CommandContext(cmd.Context(), "git", args...)
		out, err := c.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return err
	}
	if err = os.Chdir(root); err != nil {
		return err
	}
	if commit {
		current, err := run("branch", "--show-current")
		if err != nil {
			return err
		}
		if current != branch {
			return fmt.Errorf("repository is on branch %q, expected %q", current, branch)
		}
		status, err := run("status", "--porcelain")
		if err != nil {
			return err
		}
		if status != "" {
			return fmt.Errorf("repository must have a clean working tree and index")
		}
		validation := make([]globusEntry, 0, len(entries))
		for _, e := range entries {
			validation = append(validation, globusEntry{destination: e.destination})
		}
		if err = validateImportDestinations(validation); err != nil {
			return err
		}
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	name, err := cfg.GetRemoteOrDefault(remote)
	if err != nil {
		return err
	}
	logger := drslog.GetLogger()
	runtime, err := remoteruntime.New(cfg, name, logger)
	if err != nil {
		return err
	}
	org, project := runtime.Organization, runtime.ProjectId
	if org == "" || project == "" || runtime.BucketName == "" || runtime.Client == nil || !runtime.CanRegister() {
		return fmt.Errorf("remote %q must support registration and specify organization/project", name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Calypr endpoint: %s (remote %s, scope %s/%s)\n", runtime.Endpoint, name, org, project)
	if err = probeManifest(cmd.Context(), runtime.Client.InternalAPI(), entries, org, project, strip); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Probed %d objects in %s/%s\n", len(entries), org, project)
	if dry && !gitOnly {
		return nil
	}
	objects := make([]*drsapi.DrsObject, 0, len(entries))
	builder := drsobjectBuilder(runtime.BucketName, org, project, runtime.StoragePrefix)
	for _, e := range entries {
		id := uuid.NewSHA1(drsobject.UUIDNamespace, []byte(fmt.Sprintf("%s:%s", project, e.sha))).String()
		obj, err := builder.Build(e.destination, e.sha, e.size, id)
		if err != nil {
			return err
		}
		methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessUrl: &drsapi.AccessURL{Url: e.resolved}}}
		obj.AccessMethods = &methods
		objects = append(objects, obj)
	}
	defer func() {
		if retErr != nil {
			if gitOnly {
				retErr = fmt.Errorf("Git-only import incomplete; repository files or a commit may already exist: %w", retErr)
			} else {
				retErr = fmt.Errorf("import incomplete; Syfon records or repository files may already have been written: %w", retErr)
			}
		}
	}()
	if gitOnly {
		if err = copyrecords.VerifyObjects(cmd.Context(), runtime.Client.InternalAPI(), objects); err != nil {
			return fmt.Errorf("verify existing Syfon records: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Verified %d existing Syfon records\n", len(objects))
		if dry {
			return nil
		}
	} else {
		if err = copyrecords.RegisterObjects(cmd.Context(), runtime.Client.InternalAPI(), objects, org, project, logger); err != nil {
			return fmt.Errorf("register Syfon records: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Registered %d objects with Syfon\n", len(objects))
	}
	if !commit {
		return nil
	}
	paths, err := gitrepo.ResolveRepositoryPaths(cmd.Context())
	if err != nil {
		return err
	}
	tracked := make([]string, 0, len(entries))
	for i, e := range entries {
		destination := filepath.Join(root, filepath.FromSlash(e.destination))
		if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if err = writePointerFile(destination, e.sha, e.size, false); err != nil {
			return err
		}
		tracked = append(tracked, e.destination)
		if err = drsobject.WriteObject(paths.DRSObjectsDir(), objects[i], e.sha); err != nil {
			return err
		}
	}
	if err = gitrepo.TrackReadOnlyFiles(cmd.Context(), tracked); err != nil {
		return err
	}
	gitargs := []string{"-c", "filter.drs.process=", "-c", "filter.drs.clean=cat", "-c", "filter.drs.required=false", "add", "--pathspec-from-file=-", "--pathspec-file-nul"}
	var pathspec strings.Builder
	pathspec.WriteString(".gitattributes\x00")
	for _, e := range entries {
		pathspec.WriteString(e.destination)
		pathspec.WriteByte(0)
	}
	add := exec.CommandContext(cmd.Context(), "git", gitargs...)
	add.Stdin = strings.NewReader(pathspec.String())
	add.Env = append(os.Environ(), "GIT_LITERAL_PATHSPECS=1")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if _, err = run("-c", "filter.drs.process=", "-c", "filter.drs.clean=cat", "-c", "filter.drs.required=false", "-c", "core.hooksPath=/dev/null", "commit", "-m", fmt.Sprintf("Index %d existing objects", len(entries))); err != nil {
		return err
	}
	head, err := run("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created commit %s on %s\n", head, branch)
	if gitOnly {
		push := exec.CommandContext(cmd.Context(), "git", "push")
		push.Env = append(os.Environ(), "GIT_LFS_SKIP_PUSH=1")
		if out, err := push.CombinedOutput(); err != nil {
			return fmt.Errorf("commit %s created but Git push failed: %s: %w", head, strings.TrimSpace(string(out)), err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Git push completed")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Use git push to publish it")
	}
	return nil
}
