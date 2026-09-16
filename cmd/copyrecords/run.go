package copyrecords

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	batchSize = normalizeCopyBatchSize(batchSize)

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
		if err := reconcileCopyBatch(ctx, logger, &stats, dst, batch, org, project, start, overwriteName); err != nil {
			return stats, err
		}
	}

	return stats, nil
}

func copyProjectRecordsFromSourceIndex(ctx context.Context, logger *slog.Logger, src indexAPI, dst indexAPI, org, project string, batchSize int, overwriteName bool) (copyStats, error) {
	batchSize = normalizeCopyBatchSize(batchSize)

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
			batch = append(batch, rec)
		}
		stats.SourceSeen += len(batch)

		if len(batch) > 0 {
			fmt.Fprintf(os.Stderr, "copy-records: reconciling source page %d (%d new records)\n", page, len(batch))
			if err := reconcileCopyBatch(ctx, logger, &stats, dst, batch, org, project, (page-1)*batchSize, overwriteName); err != nil {
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

func reconcileCopyBatch(ctx context.Context, logger *slog.Logger, stats *copyStats, dst indexAPI, batch []copyRecord, org, project string, batchStart int, overwriteName bool) error {
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

func normalizeCopyBatchSize(batchSize int) int {
	if batchSize <= 0 {
		return defaultCopyBatchSize
	}
	return batchSize
}
