package pushsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	localcommon "github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/drsremote"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	"github.com/calypr/syfon/client/hash"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type batchSyncSession struct {
	ctx            context.Context
	rt             *pushRuntime
	filesByOID     map[string]lfs.LfsFileInfo
	oids           []string
	drsObjByOID    map[string]*drsapi.DrsObject
	existingByHash map[string][]drsapi.DrsObject
	registeredOids map[string]bool
}

type uploadCandidate struct {
	oid  string
	obj  *drsapi.DrsObject
	file lfs.LfsFileInfo
	size int64
	src  string
}

// BatchSyncForPush performs checksum-first push preparation.
func BatchSyncForPush(cl *config.GitContext, ctx context.Context, files map[string]lfs.LfsFileInfo) error {
	session := &batchSyncSession{
		ctx:            ctx,
		rt:             newPushRuntime(cl),
		drsObjByOID:    make(map[string]*drsapi.DrsObject),
		existingByHash: make(map[string][]drsapi.DrsObject),
		registeredOids: make(map[string]bool),
	}
	if len(files) == 0 {
		return nil
	}

	session.normalizeFiles(files)
	if err := session.lookupMetadata(); err != nil {
		return err
	}
	if err := session.ensureMetadataRegistered(); err != nil {
		return err
	}

	candidates, err := session.identifyUploadCandidates()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}

	return session.executeUploadPlan(candidates)
}

func (s *batchSyncSession) normalizeFiles(files map[string]lfs.LfsFileInfo) {
	s.filesByOID = make(map[string]lfs.LfsFileInfo, len(files))
	for _, f := range files {
		oid := localdrsobject.NormalizeOid(f.Oid)
		if oid == "" {
			continue
		}
		if _, exists := s.filesByOID[oid]; exists {
			continue
		}
		f.Oid = oid
		s.filesByOID[oid] = f
		s.oids = append(s.oids, oid)
	}
	sort.Strings(s.oids)
}

func (s *batchSyncSession) lookupMetadata() error {
	s.existingByHash = make(map[string][]drsapi.DrsObject, len(s.oids))
	for _, oid := range s.oids {
		objects, err := drsremote.ObjectsByHash(s.ctx, s.rt.API, oid)
		if err != nil {
			return fmt.Errorf("hash lookup failed for oid %s: %w", oid, err)
		}
		for _, obj := range objects {
			objOID := localdrsobject.NormalizeOid(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
			if objOID == "" {
				continue
			}
			s.existingByHash[objOID] = append(s.existingByHash[objOID], obj)
		}
	}
	return nil
}

func (s *batchSyncSession) ensureMetadataRegistered() error {
	toRegister := make([]drsapi.DrsObjectCandidate, 0)

	for _, oid := range s.oids {
		obj, err := s.getOrCreateDRSObjectCandidate(oid)
		if err != nil {
			return err
		}
		s.drsObjByOID[oid] = obj

		recs := s.existingByHash[oid]
		if len(recs) == 0 {
			toRegister = append(toRegister, localdrsobject.ConvertToCandidate(obj))
			s.registeredOids[oid] = true
			continue
		}
		if match, err := drsremote.FindMatchingRecord(recs, s.rt.Scope.Organization, s.rt.Scope.Project); err == nil && match != nil {
			s.drsObjByOID[oid] = match
		}
	}

	if len(toRegister) == 0 {
		return nil
	}

	s.rt.Logger.InfoContext(s.ctx, fmt.Sprintf("bulk registering %d missing records", len(toRegister)))
	registered, err := s.rt.API.Client.DRS().RegisterObjects(s.ctx, drsapi.RegisterObjectsJSONRequestBody{
		Candidates: toRegister,
	})
	if err != nil {
		return fmt.Errorf("bulk register failed: %w", err)
	}
	for i := range registered.Objects {
		obj := registered.Objects[i]
		oid := localdrsobject.NormalizeOid(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
		if oid != "" {
			copyObj := obj
			s.drsObjByOID[oid] = &copyObj
		}
	}
	return nil
}

func (s *batchSyncSession) getOrCreateDRSObjectCandidate(oid string) (*drsapi.DrsObject, error) {
	file := s.filesByOID[oid]
	if localObj, err := localdrsobject.ReadObject(localcommon.DRS_OBJS_PATH, oid); err == nil && localObj != nil {
		return scopedDRSObjectForPush(s.rt, oid, file.Name, file.Size, localObj)
	}
	stat, err := os.Stat(file.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s for oid %s: %w", file.Name, oid, err)
	}
	obj, err := scopedDRSObjectForPush(s.rt, oid, file.Name, stat.Size(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build drs object for oid %s: %w", oid, err)
	}
	return obj, nil
}

func scopedDRSObjectForPush(rt *pushRuntime, oid string, path string, size int64, existing *drsapi.DrsObject) (*drsapi.DrsObject, error) {
	if rt == nil {
		return existing, nil
	}
	if existing != nil && size <= 0 {
		size = existing.Size
	}
	if size <= 0 {
		if stat, err := os.Stat(path); err == nil {
			size = stat.Size()
		}
	}

	name := filepath.Base(path)
	if existing != nil && existing.Name != nil && *existing.Name != "" {
		name = *existing.Name
	}
	if name == "" || name == "." {
		name = oid
	}

	did := uuid.NewSHA1(localdrsobject.UUIDNamespace, []byte(fmt.Sprintf("%s:%s", rt.Scope.Project, localdrsobject.NormalizeOid(oid)))).String()
	if existing != nil && existing.Id != "" {
		did = existing.Id
	}

	obj, err := localdrsobject.BuildWithPrefix(name, oid, size, did, rt.Scope.Bucket, rt.Scope.Organization, rt.Scope.Project, rt.Scope.StoragePref)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return obj, nil
	}

	if existing.AccessMethods != nil && len(*existing.AccessMethods) > 0 {
		obj.AccessMethods = existing.AccessMethods
	}
	obj.Aliases = existing.Aliases
	obj.Contents = existing.Contents
	obj.Description = existing.Description
	obj.MimeType = existing.MimeType
	if existing.Version != nil {
		obj.Version = existing.Version
	}
	if !existing.CreatedTime.IsZero() {
		obj.CreatedTime = existing.CreatedTime
	}
	if existing.UpdatedTime != nil {
		obj.UpdatedTime = existing.UpdatedTime
	}
	return obj, nil
}

func (s *batchSyncSession) identifyUploadCandidates() ([]uploadCandidate, error) {
	candidates := make([]uploadCandidate, 0)
	for _, oid := range s.oids {
		if !s.needsUpload(oid) {
			continue
		}

		file := s.filesByOID[oid]
		srcPath, canUpload, err := resolveUploadSourcePath(oid, file.Name, file.IsPointer)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve upload source for oid %s: %w", oid, err)
		}
		if !canUpload {
			s.rt.Logger.WarnContext(s.ctx, "no local payload available; skipping upload", "oid", oid, "path", file.Name)
			continue
		}

		stat, err := os.Stat(srcPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat upload source %s: %w", srcPath, err)
		}

		candidates = append(candidates, uploadCandidate{
			oid:  oid,
			obj:  s.drsObjByOID[oid],
			file: file,
			size: stat.Size(),
			src:  srcPath,
		})
	}
	return candidates, nil
}

func (s *batchSyncSession) needsUpload(oid string) bool {
	if s.registeredOids[oid] {
		return true
	}
	if len(s.existingByHash[oid]) == 0 {
		return true
	}
	if downloadable, err := isFileDownloadable(s.rt, s.ctx, s.drsObjByOID[oid]); err != nil || !downloadable {
		return true
	}
	return false
}

func (s *batchSyncSession) executeUploadPlan(candidates []uploadCandidate) error {
	threshold := s.rt.Tuning.MultiPartThreshold
	if threshold <= 0 {
		threshold = 5 * 1024 * 1024 * 1024
	}
	concurrency := s.rt.Tuning.UploadConcurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	small, large := splitCandidatesByThreshold(candidates, threshold)
	s.rt.Logger.InfoContext(s.ctx, "upload plan prepared", "total", len(candidates), "parallel_small", len(small), "sequential_large", len(large))

	if len(small) > 0 {
		eg, egCtx := errgroup.WithContext(s.ctx)
		eg.SetLimit(concurrency)
		for _, c := range small {
			c := c
			eg.Go(func() error {
				return uploadFileForObject(s.rt, egCtx, c.obj, c.src, false)
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	for _, c := range large {
		if err := uploadFileForObject(s.rt, s.ctx, c.obj, c.src, false); err != nil {
			return err
		}
	}
	return nil
}

func splitCandidatesByThreshold(candidates []uploadCandidate, threshold int64) (small, large []uploadCandidate) {
	for _, c := range candidates {
		if c.size < threshold {
			small = append(small, c)
		} else {
			large = append(large, c)
		}
	}
	return
}
