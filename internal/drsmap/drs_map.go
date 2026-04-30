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

			authzMap := syfoncommon.AuthzMapFromScope(builder.Organization, builder.Project)
			authoritativeObj, _ = syfoncommon.EnsureAccessMethodAuthorizations(authoritativeObj, authzMap)
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
			cacheAuthzMap := syfoncommon.AuthzMapFromScope(builder.Organization, builder.Project)
			if authoritativeObj.AccessMethods != nil && len(*authoritativeObj.AccessMethods) > 0 {
				am := &(*authoritativeObj.AccessMethods)[0]
				am.AccessUrl = &struct {
					Headers *[]string `json:"headers,omitempty"`
					Url     string    `json:"url"`
				}{Url: hint}
				if cacheAuthzMap != nil {
					am.Authorizations = syfoncommon.AccessMethodAuthorizationsFromAuthzMap(cacheAuthzMap)
				}
			} else {
				newAm := drsapi.AccessMethod{
					Type: drsapi.AccessMethodTypeS3,
					AccessUrl: &struct {
						Headers *[]string `json:"headers,omitempty"`
						Url     string    `json:"url"`
					}{Url: hint},
				}
				if cacheAuthzMap != nil {
					newAm.Authorizations = syfoncommon.AccessMethodAuthorizationsFromAuthzMap(cacheAuthzMap)
				}
				authoritativeObj.AccessMethods = &[]drsapi.AccessMethod{newAm}
			}
		}

		if err := drsobject.WriteObject(common.DRS_OBJS_PATH, authoritativeObj, file.Oid); err != nil {
			opts.Logger.Error(fmt.Sprintf("could not write local DRS object for %s OID %s: %v", file.Name, file.Oid, err))
			continue
		}
		opts.Logger.Info(fmt.Sprintf("Prepared File %s OID %s with DRS ID %s for commit", file.Name, file.Oid, authoritativeObj.Id))
	}

	return nil
}
