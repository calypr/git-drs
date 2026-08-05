package copyrecords

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/calypr/syfon/client/request"
)

const defaultCopyBatchSize = 1000

type copyStats struct {
	SourceSeen int
	Created    int
	Updated    int
	Unchanged  int
	Written    int
}

func copyProjectRecords(ctx context.Context, logger *slog.Logger, source []copyRecord, dst indexAPI, org, project string, batchSize int, overwriteName bool) (copyStats, error) {
	return copyProjectRecordsWithOptions(ctx, logger, source, dst, org, project, batchSize, overwriteName, false)
}

func copyProjectRecordsWithOptions(ctx context.Context, logger *slog.Logger, source []copyRecord, dst indexAPI, org, project string, batchSize int, overwriteName, overwriteExisting bool) (copyStats, error) {
	batchSize = normalizeCopyBatchSize(batchSize)
	if overwriteExisting && batchSize > defaultCopyBatchSize {
		batchSize = defaultCopyBatchSize
	}

	stats := copyStats{}
	fmt.Fprintf(os.Stderr, "copy-records: scanning source records for %s/%s\n", org, project)
	records := source
	stats.SourceSeen = len(records)
	fmt.Fprintf(os.Stderr, "copy-records: source scan complete, %d records in scope\n", stats.SourceSeen)

	for start := 0; start < len(records); start += batchSize {
		end := start + batchSize
		if end > len(records) {
			end = len(records)
		}

		batch := records[start:end]
		fmt.Fprintf(os.Stderr, "copy-records: reconciling batch %d-%d of %d\n", start+1, end, len(records))
		if err := reconcileCopyBatchWithOptions(ctx, logger, &stats, dst, batch, org, project, start, overwriteName, overwriteExisting); err != nil {
			return stats, err
		}
	}

	return stats, nil
}

func copyProjectRecordsFromSourceIndex(ctx context.Context, logger *slog.Logger, src indexAPI, dst indexAPI, org, project string, batchSize int, overwriteName bool) (copyStats, error) {
	return copyProjectRecordsFromSourceIndexWithOptions(ctx, logger, src, dst, org, project, batchSize, overwriteName, false)
}

func copyProjectRecordsFromSourceIndexWithOptions(ctx context.Context, logger *slog.Logger, src indexAPI, dst indexAPI, org, project string, batchSize int, overwriteName, overwriteExisting bool) (copyStats, error) {
	return copyProjectRecordsFromSourceIndexWithFilter(ctx, logger, src, dst, org, project, batchSize, overwriteName, overwriteExisting, nil)
}

func copyProjectRecordsFromSourceIndexWithFilter(ctx context.Context, logger *slog.Logger, src indexAPI, dst indexAPI, org, project string, batchSize int, overwriteName, overwriteExisting bool, includedSHA256 map[string]struct{}) (copyStats, error) {
	batchSize = normalizeCopyBatchSize(batchSize)
	if overwriteExisting && batchSize > defaultCopyBatchSize {
		batchSize = defaultCopyBatchSize
	}

	stats := copyStats{}
	seen := map[string]struct{}{}
	startAfter := ""
	fmt.Fprintf(os.Stderr, "copy-records: scanning source records for %s/%s\n", org, project)

	for page := 1; ; page++ {
		fmt.Fprintf(os.Stderr, "copy-records: scanning source index page %d for %s/%s, start-after=%q matched-so-far=%d\n", page, org, project, startAfter, stats.SourceSeen)
		records, err := listSourceRecordPage(ctx, src, org, project, batchSize, startAfter)
		if err != nil {
			return stats, err
		}
		if len(records) == 0 {
			break
		}
		nextStartAfter := lastCopyRecordDID(records)
		if nextStartAfter == "" {
			return stats, fmt.Errorf("source list for %s/%s returned records without DIDs; cannot advance cursor", org, project)
		}

		batch := make([]copyRecord, 0, len(records))
		for _, rec := range records {
			did := strings.TrimSpace(rec.Did)
			if did == "" {
				continue
			}
			if _, ok := seen[did]; ok {
				continue
			}
			seen[did] = struct{}{}
			if !copyRecordMatchesIncludedSHA256(rec, includedSHA256) {
				continue
			}
			batch = append(batch, rec)
		}
		stats.SourceSeen += len(batch)

		if len(batch) > 0 {
			fmt.Fprintf(os.Stderr, "copy-records: reconciling source page %d (%d new records)\n", page, len(batch))
			if err := reconcileCopyBatchWithOptions(ctx, logger, &stats, dst, batch, org, project, (page-1)*batchSize, overwriteName, overwriteExisting); err != nil {
				return stats, err
			}
		}
		if len(records) < batchSize {
			break
		}
		startAfter = nextStartAfter
	}

	fmt.Fprintf(os.Stderr, "copy-records: source scan complete, %d records in scope\n", stats.SourceSeen)
	return stats, nil
}

func copyRecordMatchesIncludedSHA256(rec copyRecord, include map[string]struct{}) bool {
	if include == nil {
		return true
	}
	_, ok := include[strings.ToLower(copyRecordSHA256(rec))]
	return ok
}

func reconcileCopyBatch(ctx context.Context, logger *slog.Logger, stats *copyStats, dst indexAPI, batch []copyRecord, org, project string, batchStart int, overwriteName bool) error {
	return reconcileCopyBatchWithOptions(ctx, logger, stats, dst, batch, org, project, batchStart, overwriteName, false)
}

func reconcileCopyBatchWithOptions(ctx context.Context, logger *slog.Logger, stats *copyStats, dst indexAPI, batch []copyRecord, org, project string, batchStart int, overwriteName, overwriteExisting bool) error {
	if overwriteExisting {
		resp, err := overwriteCopyBatch(ctx, dst, copyBulkOverwriteRequest{Organization: org, Project: project, Records: batch})
		if err != nil {
			return fmt.Errorf("target bulk overwrite failed for batch starting at %d: %w", batchStart, err)
		}
		stats.Created += resp.Created
		stats.Updated += resp.Replaced
		stats.Written += resp.Processed
		fmt.Fprintf(os.Stderr, "copy-records: batch complete, created=%d updated=%d unchanged=0 written=%d total-written=%d\n", resp.Created, resp.Replaced, resp.Processed, stats.Written)
		return nil
	}
	toWrite, batchStats, err := buildMergedBatch(ctx, dst, batch, overwriteName)
	if err != nil {
		return err
	}
	stats.Created += batchStats.Created
	stats.Updated += batchStats.Updated
	stats.Unchanged += batchStats.Unchanged

	if len(toWrite) > 0 {
		resp, err := dst.CreateBulk(ctx, copyBulkCreateRequest{Records: toWrite})
		if err != nil {
			return fmt.Errorf("target bulk create failed for batch starting at %d: %w", batchStart, err)
		}
		if resp.Records != nil {
			stats.Written += len(*resp.Records)
		} else {
			stats.Written += len(toWrite)
		}
	}
	fmt.Fprintf(
		os.Stderr,
		"copy-records: batch complete, created=%d updated=%d unchanged=%d written=%d total-written=%d\n",
		batchStats.Created,
		batchStats.Updated,
		batchStats.Unchanged,
		len(toWrite),
		stats.Written,
	)

	if logger != nil {
		logger.Info("copy-records batch complete",
			"organization", org,
			"project", project,
			"batch_start", batchStart,
			"source_records", len(batch),
			"created", batchStats.Created,
			"updated", batchStats.Updated,
			"unchanged", batchStats.Unchanged,
			"written", len(toWrite),
		)
	}
	return nil
}

// overwriteCopyBatch retries a rejected payload as smaller atomic requests.
// Syfon rejects oversized overwrite requests before writing any record.
func overwriteCopyBatch(ctx context.Context, dst indexAPI, req copyBulkOverwriteRequest) (copyBulkOverwriteResponse, error) {
	resp, err := dst.OverwriteBulk(ctx, req)
	if err == nil {
		return resp, nil
	}
	var responseErr *request.ResponseError
	if errors.As(err, &responseErr) && (responseErr.Status == 404 || responseErr.Status == 405) {
		return copyBulkOverwriteResponse{}, fmt.Errorf("target Syfon does not support bulk overwrite; upgrade the target Syfon instance")
	}
	if len(req.Records) <= 1 || !errors.As(err, &responseErr) || responseErr.Status != 413 {
		return copyBulkOverwriteResponse{}, err
	}
	middle := len(req.Records) / 2
	left := req
	left.Records = req.Records[:middle]
	right := req
	right.Records = req.Records[middle:]
	leftResp, err := overwriteCopyBatch(ctx, dst, left)
	if err != nil {
		return copyBulkOverwriteResponse{}, err
	}
	rightResp, err := overwriteCopyBatch(ctx, dst, right)
	if err != nil {
		return copyBulkOverwriteResponse{}, err
	}
	return copyBulkOverwriteResponse{
		Processed:       leftResp.Processed + rightResp.Processed,
		Created:         leftResp.Created + rightResp.Created,
		Replaced:        leftResp.Replaced + rightResp.Replaced,
		DIDMatched:      leftResp.DIDMatched + rightResp.DIDMatched,
		ChecksumMatched: leftResp.ChecksumMatched + rightResp.ChecksumMatched,
	}, nil
}

func normalizeCopyBatchSize(batchSize int) int {
	if batchSize <= 0 {
		return defaultCopyBatchSize
	}
	return batchSize
}
