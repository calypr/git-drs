package copyrecords

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

type copyStats struct {
	SourceSeen int
	Created    int
	Updated    int
	Unchanged  int
	Written    int
}

func copyProjectRecords(ctx context.Context, logger *slog.Logger, source []copyRecord, dst indexAPI, org, project string, batchSize int, overwriteName bool) (copyStats, error) {
	if batchSize <= 0 {
		batchSize = 250
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
		toWrite, batchStats, err := buildMergedBatch(ctx, dst, batch, overwriteName)
		if err != nil {
			return stats, err
		}
		stats.Created += batchStats.Created
		stats.Updated += batchStats.Updated
		stats.Unchanged += batchStats.Unchanged

		if len(toWrite) > 0 {
			resp, err := dst.CreateBulk(ctx, copyBulkCreateRequest{Records: toWrite})
			if err != nil {
				return stats, fmt.Errorf("target bulk create failed for batch starting at %d: %w", start, err)
			}
			if resp.Records != nil {
				stats.Written += len(*resp.Records)
			} else {
				stats.Written += len(toWrite)
			}
		}
		fmt.Fprintf(
			os.Stderr,
			"copy-records: batch %d-%d complete, created=%d updated=%d unchanged=%d written=%d\n",
			start+1,
			end,
			batchStats.Created,
			batchStats.Updated,
			batchStats.Unchanged,
			len(toWrite),
		)

		if logger != nil {
			logger.Info("copy-records batch complete",
				"organization", org,
				"project", project,
				"batch_start", start,
				"source_records", len(batch),
				"created", batchStats.Created,
				"updated", batchStats.Updated,
				"unchanged", batchStats.Unchanged,
				"written", len(toWrite),
			)
		}
	}

	return stats, nil
}
