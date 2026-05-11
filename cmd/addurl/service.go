package addurl

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/drstrack"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	sycloud "github.com/calypr/syfon/client/cloud"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// AddURLService groups injectable dependencies used to implement the add-url
// behavior (logger factory, object inspection, LFS helpers, config loader, etc.).
type AddURLService struct {
	newLogger     func(string, bool) (*slog.Logger, error)
	inspectObject func(ctx context.Context, input sycloud.ObjectParameters) (*sycloud.ObjectInfo, error)
	isLFSTracked  func(path string) (bool, error)
	getGitRoots   func(ctx context.Context) (string, string, error)
	gitLFSTrack   func(ctx context.Context, path string) (bool, error)
	loadConfig    func() (*config.Config, error)
}

// NewAddURLService constructs an AddURLService populated with production
// implementations of its dependencies.
func NewAddURLService() *AddURLService {
	return &AddURLService{
		newLogger:     drslog.NewLogger,
		inspectObject: sycloud.InspectObject,
		isLFSTracked:  lfs.IsLFSTracked,
		getGitRoots:   lfs.GetGitRootDirectories,
		gitLFSTrack:   drstrack.TrackReadOnly,
		loadConfig:    config.LoadConfig,
	}
}

// Run executes the add-url workflow: parse CLI input, resolve the target bucket
// scope, inspect the provider object through the client-owned cloud package,
// ensure the LFS object exists in local storage, write a pointer file, update
// the pre-commit cache (best-effort), optionally add a tracking entry, and
// record the DRS mapping.
func (s *AddURLService) Run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	logger, err := s.newLogger("", false)
	if err != nil {
		return fmt.Errorf("error creating logger: %v", err)
	}

	input, err := parseAddURLInput(cmd, args)
	if err != nil {
		return err
	}

	cfg, err := s.loadConfig()
	if err != nil {
		return fmt.Errorf("error getting config: %v", err)
	}

	remote, err := cfg.GetDefaultRemote()
	if err != nil {
		return err
	}

	remoteConfig := cfg.GetRemote(remote)
	if remoteConfig == nil {
		return fmt.Errorf("error getting remote configuration for %s", remote)
	}

	org, project, scope, err := resolveTargetScope(remoteConfig)
	if err != nil {
		return err
	}

	input.objectURL, err = resolveObjectURL(input, scope)
	if err != nil {
		return err
	}

	objectInfo, err := s.inspectObject(ctx, buildObjectParameters(input.objectURL, input.path, input.sha256))
	if err != nil {
		return err
	}

	isTracked, err := s.isLFSTracked(input.path)
	if err != nil {
		return fmt.Errorf("check LFS tracking for %s: %w", input.path, err)
	}

	gitCommonDir, lfsRoot, err := s.getGitRoots(ctx)
	if err != nil {
		return fmt.Errorf("get git root directories: %w", err)
	}

	if err := printResolvedInfo(cmd, gitCommonDir, lfsRoot, objectInfo, input.path, isTracked, input.sha256); err != nil {
		return err
	}

	oid, err := s.ensureLFSObject(ctx, objectInfo, input, lfsRoot)
	if err != nil {
		return err
	}

	if err := writePointerFile(input.path, oid, objectInfo.SizeBytes); err != nil {
		return err
	}

	if err := updatePrecommitCache(ctx, logger, input.path, oid, input.objectURL); err != nil {
		logger.Warn("pre-commit cache update skipped", "error", err)
	}

	if err := maybeTrackLFS(ctx, s.gitLFSTrack, input.path, isTracked); err != nil {
		return err
	}

	builder := drsobject.NewBuilder(scope.Bucket, project)
	builder.Organization = org
	builder.StoragePrefix = scope.Prefix

	file := addURLDrsFile{
		Name: input.path,
		Size: objectInfo.SizeBytes,
		Oid:  oid,
	}
	if _, err := writeAddURLDrsObject(builder, file, input.objectURL); err != nil {
		return fmt.Errorf("write local DRS object: %w", err)
	}

	return nil
}

type addURLDrsFile struct {
	Name string
	Size int64
	Oid  string
}

func writeAddURLDrsObject(builder drsobject.Builder, file addURLDrsFile, objectPath string) (*drsapi.DrsObject, error) {
	existing, err := drsobject.ReadObject(common.DRS_OBJS_PATH, file.Oid)
	var drsObj *drsapi.DrsObject
	if err == nil && existing != nil {
		drsObj = existing
		name := file.Name
		drsObj.Name = &name
		drsObj.Size = file.Size
	} else {
		drsID := uuid.NewSHA1(drsobject.UUIDNamespace, []byte(fmt.Sprintf("%s:%s", builder.Project, drsobject.NormalizeOid(file.Oid)))).String()
		drsObj, err = builder.Build(file.Name, file.Oid, file.Size, drsID)
		if err != nil {
			return nil, fmt.Errorf("error building DRS object for oid %s: %w", file.Oid, err)
		}
	}

	if objectPath != "" {
		if drsObj.AccessMethods != nil && len(*drsObj.AccessMethods) > 0 {
			am := &(*drsObj.AccessMethods)[0]
			am.AccessUrl = &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: objectPath}
		} else {
			drsObj.AccessMethods = &[]drsapi.AccessMethod{{
				Type: drsapi.AccessMethodTypeS3,
				AccessUrl: &struct {
					Headers *[]string `json:"headers,omitempty"`
					Url     string    `json:"url"`
				}{Url: objectPath},
			}}
		}
	}

	if err := drsobject.WriteObject(common.DRS_OBJS_PATH, drsObj, file.Oid); err != nil {
		return nil, fmt.Errorf("error writing DRS object for oid %s: %w", file.Oid, err)
	}
	return drsObj, nil
}

// ensureLFSObject returns the LFS pointer OID to use for the add-url target.
// If SHA256 is provided, it is trusted and returned. Otherwise we derive a
// deterministic placeholder OID from provider identity without writing any
// local LFS object payload.
func (s *AddURLService) ensureLFSObject(ctx context.Context, objectInfo *sycloud.ObjectInfo, input addURLInput, lfsRoot string) (string, error) {
	_ = ctx
	_ = lfsRoot
	if input.sha256 != "" {
		return input.sha256, nil
	}

	return placeholderOIDForUnknownSHA(objectInfo.ETag, input.objectURL)
}

func placeholderOIDForUnknownSHA(etag string, sourceURL string) (string, error) {
	e := strings.TrimSpace(strings.Trim(etag, `"`))
	src := strings.TrimSpace(sourceURL)
	if e == "" {
		return "", fmt.Errorf("etag is required for placeholder oid")
	}
	if src == "" {
		return "", fmt.Errorf("source URL is required for placeholder oid")
	}
	sum := sha256.Sum256([]byte("git-drs-add-url-placeholder:v2\netag=" + e + "\nsource=" + src + "\n"))
	return fmt.Sprintf("%x", sum[:]), nil
}
