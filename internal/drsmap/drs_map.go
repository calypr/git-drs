package drsmap

import (
	"fmt"
	"log/slog"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/precommit_cache"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syfoncommon "github.com/calypr/syfon/common"
	"github.com/google/uuid"
)

type WriteOptions struct {
	Cache          *precommit_cache.Cache
	PreferCacheURL bool
	Logger         *slog.Logger
}

func WriteObjectsForLFSFiles(builder drsobject.Builder, lfsFiles map[string]lfs.LfsFileInfo, opts WriteOptions) error {
	if opts.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	opts.Logger.Debug("writing local DRS objects for LFS files")

	if builder.Project == "" {
		return fmt.Errorf("no project configured")
	}
	if len(lfsFiles) == 0 {
		return nil
	}

	for _, file := range lfsFiles {
		var authoritativeObj *drsapi.DrsObject
		existing, err := drsobject.ReadObject(common.DRS_OBJS_PATH, file.Oid)
		if err == nil && existing != nil {
			authoritativeObj = existing
			name := file.Name
			authoritativeObj.Name = &name
			authoritativeObj.Size = file.Size
			ensureControlledAccess(authoritativeObj, builder.Organization, builder.Project)
		} else {
			drsID := uuid.NewSHA1(drsobject.UUIDNamespace, []byte(fmt.Sprintf("%s:%s", builder.Project, drsobject.NormalizeOid(file.Oid)))).String()
			authoritativeObj, err = builder.Build(file.Name, file.Oid, file.Size, drsID)
			if err != nil {
				opts.Logger.Error(fmt.Sprintf("Could not build DRS object for %s OID %s %v", file.Name, file.Oid, err))
				continue
			}
		}

		authoritativeURL := ""
		if authoritativeObj.AccessMethods != nil && len(*authoritativeObj.AccessMethods) > 0 && (*authoritativeObj.AccessMethods)[0].AccessUrl != nil {
			authoritativeURL = (*authoritativeObj.AccessMethods)[0].AccessUrl.Url
		}

		var hint string
		if opts.Cache != nil {
			externalURL, ok, err := opts.Cache.LookupExternalURLByOID(file.Oid)
			if err != nil {
				opts.Logger.Debug(fmt.Sprintf("cache lookup failed for %s: %v", file.Oid, err))
			} else if ok {
				hint = externalURL
			}
		}

		if hint != "" {
			if err := precommit_cache.CheckExternalURLMismatch(hint, authoritativeURL); err != nil {
				opts.Logger.Warn(fmt.Sprintf("Warning. %s (path=%s oid=%s)", err.Error(), file.Name, file.Oid))
			}
		}

		if opts.PreferCacheURL && hint != "" {
			if authoritativeObj.AccessMethods != nil && len(*authoritativeObj.AccessMethods) > 0 {
				am := &(*authoritativeObj.AccessMethods)[0]
				am.AccessUrl = &struct {
					Headers *[]string `json:"headers,omitempty"`
					Url     string    `json:"url"`
				}{Url: hint}
			} else {
				newAm := drsapi.AccessMethod{
					Type: drsapi.AccessMethodTypeS3,
					AccessUrl: &struct {
						Headers *[]string `json:"headers,omitempty"`
						Url     string    `json:"url"`
					}{Url: hint},
				}
				authoritativeObj.AccessMethods = &[]drsapi.AccessMethod{newAm}
			}
			ensureControlledAccess(authoritativeObj, builder.Organization, builder.Project)
		}

		if err := drsobject.WriteObject(common.DRS_OBJS_PATH, authoritativeObj, file.Oid); err != nil {
			opts.Logger.Error(fmt.Sprintf("could not write local DRS object for %s OID %s: %v", file.Name, file.Oid, err))
			continue
		}
		opts.Logger.Info(fmt.Sprintf("Prepared File %s OID %s with DRS ID %s for commit", file.Name, file.Oid, authoritativeObj.Id))
	}

	return nil
}

func ensureControlledAccess(obj *drsapi.DrsObject, org, project string) {
	if obj == nil {
		return
	}
	authzMap := syfoncommon.AuthzMapFromScope(org, project)
	if len(authzMap) == 0 {
		return
	}
	next := append([]string(nil), derefStringSlice(obj.ControlledAccess)...)
	next = append(next, syfoncommon.AuthzMapToControlledAccess(authzMap)...)
	normalized := syfoncommon.NormalizeAccessResources(next)
	if len(normalized) == 0 {
		return
	}
	obj.ControlledAccess = &normalized
}

func derefStringSlice(ptr *[]string) []string {
	if ptr == nil {
		return nil
	}
	return append([]string(nil), (*ptr)...)
}
