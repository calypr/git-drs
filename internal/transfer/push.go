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
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type batchSyncSession struct {
	ctx                context.Context
	rt                 *pushRuntime
	reporter           UploadProgressReporter
	filesByOID         map[string]lfs.LfsFileInfo
	oids               []string
	drsObjByOID        map[string]*drsapi.DrsObject
	existingByHash     map[string][]drsapi.DrsObject
	uploadRequired     map[string]bool
	skippedUnavailable int
}

type PushSyncSummary struct {
	SkippedUnavailable int
}

func (s *batchSyncSession) debug(message string, args ...any) {
	if s != nil && s.rt != nil && s.rt.Logger != nil {
		s.rt.Logger.DebugContext(s.ctx, message, args...)
	}
}

type uploadCandidate struct {
	oid  string
	obj  *drsapi.DrsObject
	file lfs.LfsFileInfo
	size int64
	src  string
}

const metadataLookupBatchSize = 500
const metadataRegisterBatchSize = 250

func BatchSyncForPush(cl *remoteruntime.GitContext, ctx context.Context, files map[string]lfs.LfsFileInfo, reporter UploadProgressReporter) error {
	_, err := BatchSyncForPushWithSummary(cl, ctx, files, reporter)
	return err
}

func BatchSyncForPushWithSummary(cl *remoteruntime.GitContext, ctx context.Context, files map[string]lfs.LfsFileInfo, reporter UploadProgressReporter) (PushSyncSummary, error) {
	session := &batchSyncSession{
		ctx:            ctx,
		rt:             newPushRuntime(cl),
		reporter:       reporter,
		drsObjByOID:    make(map[string]*drsapi.DrsObject),
		existingByHash: make(map[string][]drsapi.DrsObject),
		uploadRequired: make(map[string]bool),
	}
	if len(files) == 0 {
		return PushSyncSummary{}, nil
	}

	session.debug("normalizing push files")
	session.normalizeFiles(files)
	session.debug("looking up push metadata", "objects", len(session.oids))
	if err := session.lookupMetadata(); err != nil {
		return PushSyncSummary{}, err
	}
	session.debug("ensuring push metadata registered")
	if err := session.ensureMetadataRegistered(); err != nil {
		return PushSyncSummary{}, err
	}

	session.debug("identifying upload candidates")
	candidates, err := session.identifyUploadCandidates()
	if err != nil {
		return PushSyncSummary{}, err
	}
	session.debug("identified upload candidates", "objects", len(candidates))
	if len(candidates) == 0 {
		return PushSyncSummary{SkippedUnavailable: session.skippedUnavailable}, nil
	}

	session.debug("executing upload plan")
	if err := session.executeUploadPlan(candidates); err != nil {
		return PushSyncSummary{}, err
	}
	return PushSyncSummary{SkippedUnavailable: session.skippedUnavailable}, nil
}

func (s *batchSyncSession) normalizeFiles(files map[string]lfs.LfsFileInfo) {
	s.filesByOID = make(map[string]lfs.LfsFileInfo, len(files))
	for _, f := range files {
		oid := localdrsobject.NormalizeOid(f.Oid)
		if oid == "" {
			continue
		}
		// DRS URI pointers identify an existing remote object; they are not
		// SHA-256 checksums. Leave them to the URI-aware fetch path rather than
		// sending the authority/object value through checksum lookup,
		// registration, or upload synchronization.
		if lfs.IsDRSURI(oid) {
			s.debug("skipping DRS URI pointer during checksum push synchronization", "oid", oid)
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
		fmt.Fprintf(os.Stdout, "DRS: checking remote metadata batch %d/%d (%d checksum(s), one Syfon request)\n", idx+1, len(batches), len(batch))
		s.debug("metadata lookup batch", "batch", idx+1, "batches", len(batches), "size", len(batch))
		objectsByHash, err := lookup.ObjectsByHashes(s.ctx, s.rt.API, batch)
		if err != nil {
			return fmt.Errorf("batch hash lookup failed: %w", err)
		}
		for _, oid := range batch {
			s.existingByHash[oid] = append(s.existingByHash[oid], objectsByHash[oid]...)
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
			s.debug("processing metadata object", "object", idx, "total", len(s.oids))
		}
		obj, err := s.getOrCreateDRSObjectCandidate(oid)
		if err != nil {
			return err
		}
		s.drsObjByOID[oid] = obj

		recs := s.existingByHash[oid]
		if len(recs) == 0 {
			// add-url deliberately does not place payload bytes in the local LFS
			// cache. Its locally stored DRS object is nevertheless actionable
			// because it points at an existing external object. Register that
			// metadata without scheduling an upload.
			if localObjectHasResolvableAccessMethod(oid) {
				s.drsObjByOID[oid] = obj
				toRegister = append(toRegister, s.metadataRecordForOID(oid, obj))
				s.uploadRequired[oid] = false
				continue
			}

			// A pointer in Git history is not actionable unless this checkout
			// has the payload bytes. Do not create orphan metadata for historical
			// pointers that the caller cannot upload.
			if !s.hasLocalPayload(oid) {
				s.skippedUnavailable++
				s.debug("skipping pointer without local payload", "oid", oid)
				continue
			}
			toRegister = append(toRegister, s.metadataRecordForOID(oid, obj))
			s.uploadRequired[oid] = true
			continue
		}
		if match, err := lookup.FindMatchingRecord(recs, s.rt.Scope.Organization, s.rt.Scope.Project); err == nil && match != nil {
			// The record is normally already registered in this scope, so avoid
			// rewriting it on every push. An explicit local access URL (for
			// example one created by add-url) is an intentional metadata change,
			// however, and must be propagated even when the server can already
			// resolve the object by checksum.
			localObj, readErr := localdrsobject.ReadObject(gitrepo.DRSObjectsPath, oid)
			localSHA256 := objectSHA256(obj)
			if s.filesByOID[oid].Placeholder && readErr != nil && (localSHA256 == "" || strings.EqualFold(localSHA256, oid)) {
				s.drsObjByOID[oid] = match
				s.uploadRequired[oid] = s.rt.Tuning.ForceUpload
				continue
			}
			localURL := firstAccessURL(obj)
			if readErr == nil && localObj != nil {
				localURL = firstAccessURL(localObj)
			}
			placeholderRegistered := !s.filesByOID[oid].Placeholder || hasPlaceholderChecksum(match, oid)
			remoteSHA256 := objectSHA256(match)
			if localSHA256 != "" && remoteSHA256 != "" && !strings.EqualFold(localSHA256, remoteSHA256) {
				return fmt.Errorf("local sha256 %s conflicts with remote sha256 %s for placeholder oid %s", localSHA256, remoteSHA256, oid)
			}
			if (localURL != "" && localURL != firstAccessURL(match)) || !placeholderRegistered || missingSHA256Checksum(obj, match) {
				s.drsObjByOID[oid] = obj
				toRegister = append(toRegister, s.metadataRecordForOID(oid, obj))
				s.uploadRequired[oid] = s.rt.Tuning.ForceUpload
			} else {
				s.drsObjByOID[oid] = match
				s.uploadRequired[oid] = s.rt.Tuning.ForceUpload
			}
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

	s.rt.Logger.InfoContext(s.ctx, "registering missing metadata", "records", len(toRegister))
	for start := 0; start < len(toRegister); start += metadataRegisterBatchSize {
		end := start + metadataRegisterBatchSize
		if end > len(toRegister) {
			end = len(toRegister)
		}
		batch := toRegister[start:end]
		registered, err := s.rt.API.Client.InternalAPI().InternalBulkCreateWithResponse(s.ctx, internalapi.InternalBulkCreateJSONRequestBody(
			internalapi.BulkCreateRequest{Records: batch},
		))
		if err != nil {
			return fmt.Errorf("bulk register failed for batch %d-%d: %w", start+1, end, err)
		}
		if registered.JSON201 == nil {
			return fmt.Errorf("bulk register failed for batch %d-%d: unexpected response %d", start+1, end, registered.StatusCode())
		}
		if registered.JSON201.Records == nil {
			return fmt.Errorf("bulk register failed for batch %d-%d: empty record response", start+1, end)
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
				Completed: end,
				Total:     len(toRegister),
				Phase:     MetadataProgressRegistering,
			})
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
	record := localdrsobject.ConvertToInternalRecord(obj, s.rt.Scope.Organization, s.rt.Scope.Project)
	if s.filesByOID[oid].Placeholder {
		hashes := internalapi.HashInfo{"git-drs-placeholder": oid}
		if record.Hashes != nil {
			for checksumType, checksum := range *record.Hashes {
				if strings.EqualFold(checksumType, "sha256") && localdrsobject.NormalizeOid(checksum) == oid {
					continue
				}
				hashes[checksumType] = checksum
			}
		}
		record.Hashes = &hashes
	}
	return record
}

func missingSHA256Checksum(local, remote *drsapi.DrsObject) bool {
	want := objectSHA256(local)
	return want != "" && objectSHA256(remote) == ""
}

func objectSHA256(obj *drsapi.DrsObject) string {
	if obj == nil {
		return ""
	}
	for _, checksum := range obj.Checksums {
		checksumType := strings.ToLower(strings.TrimSpace(checksum.Type))
		if checksumType == "sha256" || checksumType == "sha-256" {
			return localdrsobject.NormalizeChecksum(checksum.Checksum)
		}
	}
	return ""
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
		obj, err := scopedDRSObjectForPush(s.rt, oid, file.Name, file.Size, localObj)
		if err != nil {
			return nil, err
		}
		// add-url records carry an explicit source URL that must survive the
		// normal scoped-object normalization used for push.
		if firstAccessURL(localObj) != "" {
			obj.AccessMethods = localObj.AccessMethods
		}
		return obj, nil
	}
	size := file.Size
	if size <= 0 {
		stat, err := os.Stat(file.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to stat file %s for oid %s: %w", file.Name, oid, err)
		}
		size = stat.Size()
	}
	obj, err := scopedDRSObjectForPush(s.rt, oid, file.Name, size, nil)
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
	obj.Checksums = existing.Checksums
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
			s.skippedUnavailable++
			s.debug("skipping upload without local payload", "oid", oid, "pointer", file.Name)
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

func (s *batchSyncSession) hasLocalPayload(oid string) bool {
	file := s.filesByOID[oid]
	_, ok, err := resolveUploadSourcePath(oid, file.Name, file.IsPointer)
	return err == nil && ok
}

func (s *batchSyncSession) needsUpload(oid string) (bool, error) {
	if s.rt.Tuning.ForceUpload {
		return true, nil
	}
	return s.uploadRequired[oid], nil
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

func localObjectHasResolvableAccessMethod(oid string) bool {
	obj, err := localdrsobject.ReadObject(gitrepo.DRSObjectsPath, oid)
	return err == nil && hasResolvableAccessMethod(obj)
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
