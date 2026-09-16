package copyrecords

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func buildMergedBatch(ctx context.Context, dst indexAPI, source []copyRecord, overwriteName bool) ([]copyRecord, copyStats, error) {
	stats := copyStats{}
	if len(source) == 0 {
		return nil, stats, nil
	}

	dids := make([]string, 0, len(source))
	hashQueries := make([]string, 0, len(source))
	seenHashQueries := make(map[string]struct{}, len(source))
	for _, rec := range source {
		did := strings.TrimSpace(rec.Did)
		if did == "" {
			continue
		}
		dids = append(dids, did)
		if sha := copyRecordSHA256(rec); sha != "" {
			query := "sha256:" + sha
			if _, ok := seenHashQueries[query]; !ok {
				seenHashQueries[query] = struct{}{}
				hashQueries = append(hashQueries, query)
			}
		}
	}

	existing, err := dst.BulkDocuments(ctx, dids)
	if err != nil {
		return nil, stats, fmt.Errorf("target bulk documents failed: %w", err)
	}
	existingByDID := make(map[string]copyRecord, len(existing))
	for _, rec := range existing {
		existingByDID[strings.TrimSpace(rec.Did)] = rec
	}

	existingByChecksum := make(map[string][]copyRecord)
	if len(hashQueries) > 0 {
		hashResp, err := dst.BulkHashes(ctx, hashQueries)
		if err != nil {
			return nil, stats, fmt.Errorf("target bulk hash lookup failed: %w", err)
		}
		for _, query := range hashQueries {
			sha := strings.TrimSpace(strings.TrimPrefix(query, "sha256:"))
			if sha == "" {
				continue
			}
			existingByChecksum[sha] = dedupeCopyRecordsByDID(hashResp.Results[query])
		}
	}

	created := make([]copyRecord, 0, len(source))
	pendingUpdates := make(map[string]copyRecord, len(source))
	updateOrder := make([]string, 0, len(source))
	for _, src := range source {
		match, found, err := targetRecordForSource(src, existingByDID, existingByChecksum)
		if err != nil {
			return nil, stats, err
		}
		if !found {
			created = append(created, src)
			stats.Created++
			continue
		}

		base := match
		targetDID := strings.TrimSpace(match.Did)
		if pending, ok := pendingUpdates[targetDID]; ok {
			base = pending
		}
		merged, changed := mergeExistingRecord(base, src, overwriteName)
		if changed {
			if _, ok := pendingUpdates[targetDID]; !ok {
				updateOrder = append(updateOrder, targetDID)
			}
			pendingUpdates[targetDID] = merged
			stats.Updated++
		} else {
			stats.Unchanged++
		}
	}

	out := make([]copyRecord, 0, len(created)+len(pendingUpdates))
	for _, did := range updateOrder {
		out = append(out, pendingUpdates[did])
	}
	out = append(out, created...)
	return out, stats, nil
}

func targetRecordForSource(src copyRecord, existingByDID map[string]copyRecord, existingByChecksum map[string][]copyRecord) (copyRecord, bool, error) {
	did := strings.TrimSpace(src.Did)
	if did == "" {
		return copyRecord{}, false, nil
	}
	if dstRec, ok := existingByDID[did]; ok {
		return dstRec, true, nil
	}

	sha := copyRecordSHA256(src)
	if sha == "" {
		return copyRecord{}, false, nil
	}
	matches := existingByChecksum[sha]
	switch len(matches) {
	case 0:
		return copyRecord{}, false, nil
	case 1:
		return matches[0], true, nil
	default:
		dids := make([]string, 0, len(matches))
		for _, match := range matches {
			if did := strings.TrimSpace(match.Did); did != "" {
				dids = append(dids, did)
			}
		}
		return copyRecord{}, false, fmt.Errorf("target already has multiple records for sha256 %q under different DIDs: %s", sha, strings.Join(dids, ", "))
	}
}

func copyRecordSHA256(rec copyRecord) string {
	if rec.Hashes == nil {
		return ""
	}
	return strings.TrimSpace((*rec.Hashes)["sha256"])
}

func dedupeCopyRecordsByDID(records []copyRecord) []copyRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]copyRecord, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		did := strings.TrimSpace(rec.Did)
		if did == "" {
			continue
		}
		if _, ok := seen[did]; ok {
			continue
		}
		seen[did] = struct{}{}
		out = append(out, rec)
	}
	return out
}

func mergeExistingRecord(dst, src copyRecord, overwriteName bool) (copyRecord, bool) {
	merged := dst
	changed := false

	if overwriteName {
		if !equalStringValuePointers(merged.Name, src.Name) {
			merged.Name = src.Name
			changed = true
		}
	}

	controlledAccess := mergeStringLists(dst.ControlledAccess, src.ControlledAccess)
	if !equalStringPointers(merged.ControlledAccess, controlledAccess) {
		merged.ControlledAccess = controlledAccess
		changed = true
	}

	accessMethods := mergeAccessMethods(dst.AccessMethods, src.AccessMethods)
	if !equalAccessMethodPointers(merged.AccessMethods, accessMethods) {
		merged.AccessMethods = accessMethods
		changed = true
	}

	return merged, changed
}

func mergeStringLists(left, right *[]string) *[]string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, list := range []*[]string{left, right} {
		if list == nil {
			continue
		}
		for _, raw := range *list {
			val := strings.TrimSpace(raw)
			if val == "" {
				continue
			}
			if _, ok := seen[val]; ok {
				continue
			}
			seen[val] = struct{}{}
			out = append(out, val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func mergeAccessMethods(left, right *[]drsapi.AccessMethod) *[]drsapi.AccessMethod {
	seen := map[string]struct{}{}
	out := make([]drsapi.AccessMethod, 0)
	for _, list := range []*[]drsapi.AccessMethod{left, right} {
		if list == nil {
			continue
		}
		for _, method := range *list {
			key := canonicalAccessMethod(method)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, method)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return &out
}

func equalStringPointers(a, b *[]string) bool {
	return equalJSON(a, b)
}

func equalStringValuePointers(a, b *string) bool {
	return equalJSON(a, b)
}

func equalAccessMethodPointers(a, b *[]drsapi.AccessMethod) bool {
	return equalJSON(a, b)
}

func equalJSON(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
