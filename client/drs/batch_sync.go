package drs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/syfon/client/drs"
	"github.com/calypr/syfon/client/pkg/common"
	"github.com/calypr/syfon/client/pkg/hash"
	"golang.org/x/sync/errgroup"
)

type batchSyncSession struct {
	ctx            context.Context
	rt             *pushRuntime
	filesByOID     map[string]lfs.LfsFileInfo
	oids           []string
	drsObjByOID    map[string]*drs.DRSObject
	existingByHash map[string][]drs.DRSObject
	registeredOids map[string]bool
}

type uploadCandidate struct {
	oid  string
	obj  *drs.DRSObject
	file lfs.LfsFileInfo
	size int64
	src  string
}

// BatchSyncForPush performs checksum-first push preparation.
func BatchSyncForPush(cl *client.GitContext, ctx context.Context, files map[string]lfs.LfsFileInfo) error {
	session := &batchSyncSession{
		ctx:            ctx,
		rt:             newPushRuntime(cl),
		drsObjByOID:    make(map[string]*drs.DRSObject),
		existingByHash: make(map[string][]drs.DRSObject),
		registeredOids: make(map[string]bool),
	}
	if len(files) == 0 {
		return nil
	}

	// 1. Preparation: Normalize input and initial bulk lookup.
	session.normalizeFiles(files)
	if err := session.lookupMetadata(); err != nil {
		return err
	}

	// 2. Metadata: Resolve OIDs to DRS objects and register missing ones.
	if err := session.ensureMetadataRegistered(); err != nil {
		return err
	}

	// 3. Filtering: Decide which files actually need storage uploads.
	candidates, err := session.identifyUploadCandidates()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}

	// 4. Execution: Perform the actual transfers.
	return session.executeUploadPlan(candidates)
}

func (s *batchSyncSession) normalizeFiles(files map[string]lfs.LfsFileInfo) {
	s.filesByOID = make(map[string]lfs.LfsFileInfo, len(files))
	for _, f := range files {
		oid := drs.NormalizeOid(f.Oid)
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
	existing, err := s.rt.API.BatchGetObjectsByHash(s.ctx, s.oids)
	if err != nil {
		return fmt.Errorf("bulk hash lookup failed: %w", err)
	}
	s.existingByHash = existing
	return nil
}

func (s *batchSyncSession) ensureMetadataRegistered() error {
	toRegister := make([]*drs.DRSObject, 0)

	for _, oid := range s.oids {
		obj, err := s.getOrCreateDRSObjectCandidate(oid)
		if err != nil {
			return err
		}
		s.drsObjByOID[oid] = obj

		recs := s.existingByHash[oid]
		if len(recs) == 0 {
			toRegister = append(toRegister, obj)
			s.registeredOids[oid] = true
			continue
		}
		if match, err := drsmap.FindMatchingRecord(recs, s.rt.Scope.Organization, s.rt.Scope.ProjectID); err == nil && match != nil {
			s.drsObjByOID[oid] = match
		}
	}

	if len(toRegister) > 0 {
		s.rt.Logger.InfoContext(s.ctx, fmt.Sprintf("bulk registering %d missing records", len(toRegister)))
		registered, err := s.rt.API.RegisterRecords(s.ctx, toRegister)
		if err != nil {
			return fmt.Errorf("bulk register failed: %w", err)
		}
		for _, obj := range registered {
			if obj != nil {
				oid := drs.NormalizeOid(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
				if oid != "" {
					s.drsObjByOID[oid] = obj
				}
			}
		}
	}
	return nil
}

func (s *batchSyncSession) getOrCreateDRSObjectCandidate(oid string) (*drs.DRSObject, error) {
	if localObj, err := drsmap.DrsInfoFromOid(oid); err == nil && localObj != nil {
		return localObj, nil
	}
	file := s.filesByOID[oid]
	stat, err := os.Stat(file.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s for oid %s: %w", file.Name, oid, err)
	}
	did := drsmap.DrsUUID(s.rt.Scope.ProjectID, oid)
	obj, err := BuildDrsObj(filepath.Base(file.Name), oid, stat.Size(), did, s.rt.Scope.Bucket, s.rt.Scope.Organization, s.rt.Scope.ProjectID, s.rt.Scope.StoragePref)
	if err != nil {
		return nil, fmt.Errorf("failed to build drs object for oid %s: %w", oid, err)
	}
	return obj, nil
}

func (s *batchSyncSession) identifyUploadCandidates() ([]uploadCandidate, error) {
	validityByHash, _ := getSHA256ValidityMapRuntime(s.rt, s.ctx, s.oids)

	candidates := make([]uploadCandidate, 0)
	for _, oid := range s.oids {
		if !s.needsUpload(oid, validityByHash) {
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

func (s *batchSyncSession) needsUpload(oid string, validity map[string]bool) bool {
	if s.registeredOids[oid] {
		return true
	}
	if validity == nil {
		return len(s.existingByHash[oid]) == 0
	}
	if !validity[oid] {
		return true
	}
	// Verify downloadability if reported present by validity check.
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

	presignedURLs := s.resolveBatchUploadURLs(candidates)
	small, large := splitCandidatesByThreshold(candidates, threshold)
	s.rt.Logger.InfoContext(s.ctx, "upload plan prepared", "total", len(candidates), "parallel_small", len(small), "sequential_large", len(large))

	if len(small) > 0 {
		eg, egCtx := errgroup.WithContext(s.ctx)
		eg.SetLimit(concurrency)
		for _, c := range small {
			c := c
			eg.Go(func() error {
				key := uploadKeyFromObject(c.obj, s.rt.Scope.Bucket)
				return uploadFileForObject(s.rt, egCtx, c.obj, c.src, false, presignedURLs[c.obj.Id+"|"+key])
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	for _, c := range large {
		if err := uploadFileForObject(s.rt, s.ctx, c.obj, c.src, false, ""); err != nil {
			return err
		}
	}
	return nil
}

func (s *batchSyncSession) resolveBatchUploadURLs(candidates []uploadCandidate) map[string]string {
	urls := make(map[string]string)
	batchReqs := make([]common.UploadURLResolveRequest, 0, len(candidates))
	for _, c := range candidates {
		batchReqs = append(batchReqs, common.UploadURLResolveRequest{
			GUID:     c.obj.Id,
			Filename: uploadKeyFromObject(c.obj, s.rt.Scope.Bucket),
			Bucket:   s.rt.Scope.Bucket,
		})
	}
	if len(batchReqs) == 0 {
		return urls
	}

	resolver, ok := s.rt.API.(interface {
		ResolveUploadURLs(context.Context, []common.UploadURLResolveRequest) ([]common.UploadURLResolveResponse, error)
	})
	if !ok {
		s.rt.Logger.DebugContext(s.ctx, "batch upload URL resolve unavailable on API; falling back to per-object resolve")
		return urls
	}
	resolved, err := resolver.ResolveUploadURLs(s.ctx, batchReqs)
	if err != nil {
		s.rt.Logger.WarnContext(s.ctx, "batch upload URL resolve failed", "error", err)
		return urls
	}
	for _, res := range resolved {
		if res.Status >= 200 && res.Status < 300 && res.URL != "" {
			urls[res.GUID+"|"+res.Filename] = res.URL
		}
	}
	return urls
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
