package copyrecords

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syservices "github.com/calypr/syfon/client/services"
)

type localIndexAPI struct{}

func (localIndexAPI) List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error) {
	return copyListRecordsResponse{}, fmt.Errorf("local target does not support source listing")
}

func (localIndexAPI) BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error) {
	return nil, nil
}

func (localIndexAPI) BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error) {
	results := make(map[string][]copyRecord, len(hashes))
	for _, raw := range hashes {
		query := strings.TrimSpace(raw)
		oid := strings.TrimPrefix(query, "sha256:")
		if oid == "" {
			continue
		}
		obj, err := readLocalDRSObject(oid)
		if err != nil {
			continue
		}
		results[query] = []copyRecord{copyRecordFromLocalObject(obj)}
	}
	return copyBulkHashesResponse{Results: results}, nil
}

func (localIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	written := make([]copyRecord, 0, len(req.Records))
	skippedMissingHash := 0
	for _, rec := range req.Records {
		oid := localObjectKeyForCopyRecord(rec)
		if oid == "" {
			skippedMissingHash++
			continue
		}
		obj := localObjectFromCopyRecord(rec)
		if err := writeLocalDRSObject(oid, obj); err != nil {
			return copyListRecordsResponse{}, fmt.Errorf("write local DRS object for oid %s: %w", oid, err)
		}
		written = append(written, rec)
	}
	if skippedMissingHash > 0 {
		fmt.Fprintf(os.Stderr, "copy-records: skipped %d source records that cannot be written locally because they have no valid sha256 hash\n", skippedMissingHash)
	}
	return copyListRecordsResponse{Records: &written}, nil
}

func localObjectKeyForCopyRecord(rec copyRecord) string {
	if sha := copyRecordSHA256(rec); isHexSHA256(sha) {
		return sha
	}
	return ""
}

func isHexSHA256(raw string) bool {
	if len(raw) != 64 {
		return false
	}
	for _, r := range raw {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func localObjectFromCopyRecord(rec copyRecord) *drsapi.DrsObject {
	checksums := make([]drsapi.Checksum, 0)
	if rec.Hashes != nil {
		for typ, sum := range *rec.Hashes {
			typ = strings.TrimSpace(typ)
			sum = strings.TrimSpace(sum)
			if typ == "" || sum == "" {
				continue
			}
			checksums = append(checksums, drsapi.Checksum{Type: typ, Checksum: strings.TrimPrefix(sum, typ+":")})
		}
	}
	if len(checksums) == 0 {
		if sha := copyRecordSHA256(rec); sha != "" {
			checksums = append(checksums, drsapi.Checksum{Type: "sha256", Checksum: sha})
		}
	}

	obj := &drsapi.DrsObject{
		AccessMethods:    rec.AccessMethods,
		Checksums:        checksums,
		ControlledAccess: rec.ControlledAccess,
		Description:      rec.Description,
		Id:               strings.TrimSpace(rec.Did),
		Name:             rec.Name,
		SelfUri:          "drs://" + strings.TrimSpace(rec.Did),
		Version:          rec.Version,
	}
	if rec.Size != nil {
		obj.Size = *rec.Size
	}
	if created := parseCopyRecordTime(rec.CreatedTime); created != nil {
		obj.CreatedTime = *created
	}
	obj.UpdatedTime = parseCopyRecordTime(rec.UpdatedTime)
	return obj
}

func parseCopyRecordTime(raw *string) *time.Time {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05 -0700 MST"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return &parsed
		}
	}
	return nil
}
