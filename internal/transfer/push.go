package transfer

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/lookup"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	internalapi "github.com/calypr/syfon/apigen/client/internalapi"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/hash"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type batchSyncSession struct {
	ctx            context.Context
	rt             *pushRuntime
	reporter       UploadProgressReporter
	filesByOID     map[string]lfs.LfsFileInfo
	oids           []string
	drsObjByOID    map[string]*drsapi.DrsObject
	existingByHash map[string][]drsapi.DrsObject
	uploadRequired map[string]bool
}

type uploadCandidate struct {
	oid  string
	obj  *drsapi.DrsObject
	file lfs.LfsFileInfo
	size int64
	src  string
}

const metadataLookupBatchSize = 500

func BatchSyncForPush(cl *remoteruntime.GitContext, ctx context.Context, files map[string]lfs.LfsFileInfo, reporter UploadProgressReporter) error {
	session := &batchSyncSession{
		ctx:            ctx,
		rt:             newPushRuntime(cl),
		reporter:       reporter,
		drsObjByOID:    make(map[string]*drsapi.DrsObject),
		existingByHash: make(map[string][]drsapi.DrsObject),
		uploadRequired: make(map[string]bool),
	}
	if len(files) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush: Normalizing files...")
	session.normalizeFiles(files)
	fmt.Fprintf(os.Stderr, "DEBUG: BatchSyncForPush: Looking up metadata for %d unique OIDs...\n", len(session.oids))
	if err := session.lookupMetadata(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush: Ensuring metadata registered...")
	if err := session.ensureMetadataRegistered(); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush: Identifying upload candidates...")
	candidates, err := session.identifyUploadCandidates()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "DEBUG: BatchSyncForPush: Identified %d upload candidates\n", len(candidates))
	if len(candidates) == 0 {
		return nil
	}

	fmt.Fprintln(os.Stderr, "DEBUG: BatchSyncForPush: Executing upload plan...")
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
	batches := chunkStrings(s.oids, metadataLookupBatchSize)
	for idx, batch := range batches {
		fmt.Fprintf(os.Stderr, "DEBUG:   lookupMetadata batch %d/%d (size: %d)\n", idx+1, len(batches), len(batch))
		objectsByHash, err := lookup.ObjectsByHashes(s.ctx, s.rt.API, batch)
		if err != nil {
			return fmt.Errorf("batch hash lookup failed: %w", err)
		}
		for _, oid := range batch {
			objects := objectsByHash[oid]
			for _, obj := range objects {
				objOID := localdrsobject.NormalizeOid(hash.ConvertDrsChecksumsToHashInfo(obj.Checksums).SHA256)
				if objOID == "" {
					continue
				}
				s.existingByHash[objOID] = append(s.existingByHash[objOID], obj)
			}
		}
	}
	return nil
}

func chunkStrings(items []string, size int) [][]string {
	if len(items) == 0 {
		return nil
	}
	if size <= 0 || len(items) <= size {
		return [][]string{items}
	}
	chunks := make([][]string, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

func (s *batchSyncSession) ensureMetadataRegistered() error {
	toRegister := make([]internalapi.InternalRecord, 0)

	for idx, oid := range s.oids {
		if idx > 0 && idx%500 == 0 {
			fmt.Fprintf(os.Stderr, "DEBUG:   ensureMetadataRegistered processing object %d/%d\n", idx, len(s.oids))
		}
		obj, err := s.getOrCreateDRSObjectCandidate(oid)
		if err != nil {
			return err
		}
		s.drsObjByOID[oid] = obj

		recs := s.existingByHash[oid]
		if len(recs) == 0 {
			toRegister = append(toRegister, s.metadataRecordForOID(oid, obj))
			s.uploadRequired[oid] = true
			continue
		}
		if match, err := lookup.FindMatchingRecord(recs, s.rt.Scope.Organization, s.rt.Scope.Project); err == nil && match != nil {
			toRegister = append(toRegister, s.metadataRecordForOID(oid, obj))
			s.uploadRequired[oid] = s.rt.Tuning.ForceUpload
			continue
		}

		reusable := s.findReusableRecord(recs)
		if reusable != nil && !s.rt.Tuning.ForceUpload {
			reuseObj, err := s.buildReusableScopedObject(oid, reusable)
			if err != nil {
				return err
			}
			s.drsObjByOID[oid] = reuseObj
			toRegister = append(toRegister, s.metadataRecordForOID(oid, reuseObj))
			continue
		}

		toRegister = append(toRegister, s.metadataRecordForOID(oid, obj))
		s.uploadRequired[oid] = true
	}

	if len(toRegister) == 0 {
		return nil
	}

	if s.reporter != nil {
		s.reporter.OnMetadataPlan(MetadataPlanSummary{TotalObjects: len(toRegister)})
		s.reporter.OnMetadataProgress(MetadataProgressEvent{
			Completed: 0,
			Total:     len(toRegister),
			Phase:     MetadataProgressRegistering,
		})
	}

	s.rt.Logger.InfoContext(s.ctx, fmt.Sprintf("bulk registering %d missing records", len(toRegister)))
	registered, err := s.rt.API.Client.InternalAPI().InternalBulkCreateWithResponse(s.ctx, internalapi.InternalBulkCreateJSONRequestBody(
		internalapi.BulkCreateRequest{Records: toRegister},
	))
	if err != nil {
		return fmt.Errorf("bulk register failed: %w", err)
	}
	if registered.JSON201 == nil {
		return fmt.Errorf("bulk register failed: unexpected response %d", registered.StatusCode())
	}
	if registered.JSON201.Records == nil {
		return fmt.Errorf("bulk register failed: empty record response")
	}
	for i := range *registered.JSON201.Records {
		rec := (*registered.JSON201.Records)[i]
		oid := ""
		if rec.Hashes != nil {
			oid = localdrsobject.NormalizeOid((*rec.Hashes)["sha256"])
		}
		if oid == "" {
			continue
		}
		if obj := s.drsObjByOID[oid]; obj != nil && strings.TrimSpace(obj.Id) == "" {
			obj.Id = strings.TrimSpace(rec.Did)
		}
	}
	if s.reporter != nil {
		s.reporter.OnMetadataProgress(MetadataProgressEvent{
			Completed: len(toRegister),
			Total:     len(toRegister),
			Phase:     MetadataProgressCompleted,
		})
	}
	return nil
}

func (s *batchSyncSession) metadataRecordForOID(oid string, obj *drsapi.DrsObject) internalapi.InternalRecord {
	file := s.filesByOID[oid]
	return localdrsobject.ConvertToInternalRecord(obj, file.Name, s.rt.Scope.Organization, s.rt.Scope.Project)
}

func (s *batchSyncSession) findReusableRecord(records []drsapi.DrsObject) *drsapi.DrsObject {
	for i := range records {
		record := records[i]
		if hasResolvableAccessMethod(&record) {
			return &record
		}
	}
	return nil
}

func (s *batchSyncSession) buildReusableScopedObject(oid string, existing *drsapi.DrsObject) (*drsapi.DrsObject, error) {
	file := s.filesByOID[oid]
	obj, err := scopedDRSObjectForPush(s.rt, oid, file.Name, file.Size, existing)
	if err != nil {
		return nil, fmt.Errorf("failed to build scoped reusable object for oid %s: %w", oid, err)
	}
	if existing != nil && existing.AccessMethods != nil {
		obj.AccessMethods = existing.AccessMethods
	}
	return obj, nil
}

func (s *batchSyncSession) getOrCreateDRSObjectCandidate(oid string) (*drsapi.DrsObject, error) {
	file := s.filesByOID[oid]
	if localObj, err := localdrsobject.ReadObject(gitrepo.DRSObjectsPath, oid); err == nil && localObj != nil {
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

	name := strings.TrimSpace(filepath.Base(path))
	if name == "" || name == "." {
		if existing != nil && existing.Name != nil && strings.TrimSpace(*existing.Name) != "" {
			name = strings.TrimSpace(*existing.Name)
		}
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

	if shouldPreserveExistingAccessMethodsForPush(existing, obj, oid) {
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

func shouldPreserveExistingAccessMethodsForPush(existing *drsapi.DrsObject, generated *drsapi.DrsObject, oid string) bool {
	existingURL := firstAccessURL(existing)
	if existingURL == "" {
		return false
	}
	generatedURL := firstAccessURL(generated)
	if existingURL == generatedURL {
		return true
	}
	existingBucket, existingKey, existingOK := parseStorageURL(existingURL)
	if !existingOK {
		return true
	}
	generatedBucket, generatedKey, generatedOK := parseStorageURL(generatedURL)
	normalizedOID := strings.Trim(strings.TrimPrefix(strings.TrimSpace(oid), "sha256:"), "/")
	existingKey = strings.Trim(existingKey, "/")
	if strings.EqualFold(existingBucket, "objects") {
		return false
	}
	if generatedOK && existingKey == "" {
		return false
	}
	if generatedOK && !strings.EqualFold(existingBucket, generatedBucket) && existingKey == normalizedOID {
		return false
	}
	if generatedOK && existingKey == generatedKey && strings.EqualFold(existingBucket, generatedBucket) {
		return true
	}
	return true
}

func firstAccessURL(obj *drsapi.DrsObject) string {
	if obj == nil || obj.AccessMethods == nil || len(*obj.AccessMethods) == 0 {
		return ""
	}
	am := (*obj.AccessMethods)[0]
	if am.AccessUrl == nil {
		return ""
	}
	return strings.TrimSpace(am.AccessUrl.Url)
}

func parseStorageURL(raw string) (bucket string, key string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "s3", "gs", "azblob":
		return u.Host, strings.Trim(u.Path, "/"), true
	default:
		return "", "", false
	}
}

func (s *batchSyncSession) identifyUploadCandidates() ([]uploadCandidate, error) {
	candidates := make([]uploadCandidate, 0)
	for _, oid := range s.oids {
		needsUpload, err := s.needsUpload(oid)
		if err != nil {
			return nil, err
		}
		if !needsUpload {
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

func (s *batchSyncSession) needsUpload(oid string) (bool, error) {
	if s.rt.Tuning.ForceUpload {
		return true, nil
	}
	if s.uploadRequired[oid] {
		return true, nil
	}
	if len(s.existingByHash[oid]) == 0 {
		return true, nil
	}
	return false, nil
}

func hasResolvableAccessMethod(obj *drsapi.DrsObject) bool {
	if obj == nil || obj.AccessMethods == nil || len(*obj.AccessMethods) == 0 {
		return false
	}
	for _, am := range *obj.AccessMethods {
		if strings.TrimSpace(string(am.Type)) == "" || am.AccessUrl == nil {
			continue
		}
		if strings.TrimSpace(am.AccessUrl.Url) != "" {
			return true
		}
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
	if s.reporter != nil {
		s.reporter.OnUploadPlan(buildUploadPlanSummary(candidates))
	}

	if len(small) > 0 {
		eg, egCtx := errgroup.WithContext(s.ctx)
		eg.SetLimit(concurrency)
		for _, c := range small {
			c := c
			eg.Go(func() error {
				s.reportUploadStarted(c)
				uploadCtx := s.progressContextForCandidate(egCtx, c)
				if err := uploadFileForObject(s.rt, uploadCtx, c.obj, c.src, false); err != nil {
					return err
				}
				s.reportUploadCompleted(c)
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}
	}

	for _, c := range large {
		s.reportUploadStarted(c)
		uploadCtx := s.progressContextForCandidate(s.ctx, c)
		if err := uploadFileForObject(s.rt, uploadCtx, c.obj, c.src, false); err != nil {
			return err
		}
		s.reportUploadCompleted(c)
	}
	return nil
}

func buildUploadPlanSummary(candidates []uploadCandidate) UploadPlanSummary {
	files := make([]UploadPlanFile, 0, len(candidates))
	var totalBytes int64
	for _, c := range candidates {
		files = append(files, UploadPlanFile{
			OID:   c.oid,
			Path:  c.file.Name,
			Bytes: c.size,
		})
		totalBytes += c.size
	}
	return UploadPlanSummary{
		Files:      files,
		TotalFiles: len(files),
		TotalBytes: totalBytes,
	}
}

func (s *batchSyncSession) progressContextForCandidate(ctx context.Context, c uploadCandidate) context.Context {
	if s.reporter == nil {
		return ctx
	}
	ctx = sycommon.WithOid(ctx, c.oid)
	return sycommon.WithProgress(ctx, func(ev sycommon.ProgressEvent) error {
		if ev.Event != "progress" {
			return nil
		}
		s.reporter.OnUploadProgress(UploadProgressEvent{
			OID:            c.oid,
			Path:           c.file.Name,
			BytesSoFar:     ev.BytesSoFar,
			BytesSinceLast: ev.BytesSinceLast,
			TotalBytes:     c.size,
			Phase:          UploadProgressUploading,
		})
		return nil
	})
}

func (s *batchSyncSession) reportUploadStarted(c uploadCandidate) {
	if s.reporter == nil {
		return
	}
	s.reporter.OnUploadProgress(UploadProgressEvent{
		OID:        c.oid,
		Path:       c.file.Name,
		BytesSoFar: 0,
		TotalBytes: c.size,
		Phase:      UploadProgressUploading,
	})
}

func (s *batchSyncSession) reportUploadCompleted(c uploadCandidate) {
	if s.reporter == nil {
		return
	}
	s.reporter.OnUploadProgress(UploadProgressEvent{
		OID:        c.oid,
		Path:       c.file.Name,
		BytesSoFar: c.size,
		TotalBytes: c.size,
		Phase:      UploadProgressCompleted,
	})
}

func splitCandidatesByThreshold(candidates []uploadCandidate, threshold int64) (small, large []uploadCandidate) {
	for _, c := range candidates {
		if threshold > 0 && c.size >= threshold {
			large = append(large, c)
			continue
		}
		small = append(small, c)
	}
	return small, large
}
