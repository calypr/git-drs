# Git-DRS Migration Scripts

This directory contains tools for migrating Git-DRS projects from v1 UUIDs (project-based) to v2 UUIDs (path-based).

## Overview

The migration uses a **dual-UUID period** strategy:
- Old v1 UUIDs continue to work during transition
- New v2 UUIDs are created for all files
- Both UUIDs point to the same underlying data
- External systems can transition at their own pace

## Scripts

### 1. migrate_uuids.go

Main migration tool that creates v2 UUID indexd records for all LFS files in a project.

**Usage:**
```bash
# Dry run (recommended first step)
go run scripts/migrate_uuids.go \
  --project gdc-mirror \
  --repo /path/to/gdc-mirror \
  --dry-run \
  --output migration-report.json

# Actual migration
go run scripts/migrate_uuids.go \
  --project gdc-mirror \
  --repo /path/to/gdc-mirror \
  --output migration-report.json \
  --verbose
```

**Flags:**
- `--project`: Project ID in format `program-project` (required)
- `--repo`: Path to Git repository (default: current directory)
- `--dry-run`: Test migration without making changes
- `--output`: Export migration report to JSON file
- `--verbose`: Enable detailed logging

**Output:**
```json
{
  "project_id": "gdc-mirror",
  "start_time": "2024-01-15T10:00:00Z",
  "end_time": "2024-01-15T10:15:00Z",
  "dry_run": false,
  "total_files": 1234,
  "migrated": 1234,
  "skipped": 0,
  "errors": 0,
  "uuid_mappings": [
    {
      "file_path": "data/sample.bam",
      "sha256": "abc123...",
      "size": 12345,
      "legacy_uuid": "old-uuid-here",
      "new_uuid": "new-uuid-here",
      "migrated_at": "2024-01-15T10:05:00Z",
      "status": "created"
    }
  ]
}
```

### 2. validate_migration.go

Validation tool that verifies migration success by checking:
- New v2 UUIDs exist in indexd
- Legacy v1 UUIDs still exist (optional)
- Files are downloadable

**Usage:**
```bash
go run scripts/validate_migration.go \
  --project gdc-mirror \
  --mapping migration-report.json \
  --verbose
```

**Flags:**
- `--project`: Project ID (required)
- `--mapping`: Path to migration report JSON (required)
- `--verbose`: Enable detailed logging

**Output:**
```
================================================================================
VALIDATION RESULTS
================================================================================
Project: gdc-mirror
Validation time: 2024-01-15T10:30:00Z

Total files validated:     1234
Passed:                    1234
Warnings:                  0
Failed:                    0

Success rate: 100.0%

✓ Migration validation PASSED - All files accessible
================================================================================
```

## Migration Workflow

### Step 1: Preparation
1. Ensure you have access to the project repository
2. Verify Git-DRS configuration is correct
3. Create a backup of indexd (via indexd admin tools)

### Step 2: Dry Run
```bash
# Test migration without making changes
go run scripts/migrate_uuids.go \
  --project myproject-mydata \
  --repo /path/to/repo \
  --dry-run \
  --output dry-run-report.json

# Review the report
cat dry-run-report.json | jq .
```

**Check for:**
- Expected number of files
- No unexpected errors
- UUID mappings look correct

### Step 3: Execute Migration
```bash
# Run actual migration
go run scripts/migrate_uuids.go \
  --project myproject-mydata \
  --repo /path/to/repo \
  --output migration-report.json \
  --verbose

# Save the migration report - you'll need it!
cp migration-report.json ~/backups/myproject-migration-$(date +%Y%m%d).json
```

**Monitor output for:**
- Success rate (should be close to 100%)
- Any error messages
- Total time (estimate: ~1 sec per 10 files)

### Step 4: Validation
```bash
# Validate migration succeeded
go run scripts/validate_migration.go \
  --project myproject-mydata \
  --mapping migration-report.json

# Should see "PASSED" message
```

### Step 5: Update External Systems
Use the `migration-report.json` file to update external systems:

**Example: Update Forge metadata**
```python
import json

# Load UUID mappings
with open('migration-report.json') as f:
    report = json.load(f)

# Create lookup table
uuid_map = {
    m['legacy_uuid']: m['new_uuid']
    for m in report['uuid_mappings']
    if m['legacy_uuid']
}

# Update your database
for old_uuid, new_uuid in uuid_map.items():
    # UPDATE fhir_documents SET uuid = new_uuid WHERE uuid = old_uuid
    pass
```

### Step 6: Monitor
After migration, monitor for:
- Download success rates
- Any error logs mentioning UUIDs
- User reports of access issues

## Troubleshooting

### Error: "project ID is required"
**Cause:** Missing `--project` flag
**Fix:** Add `--project program-project` flag

### Error: "not a git repository"
**Cause:** Repository path is invalid
**Fix:** Verify path with `git -C /path/to/repo status`

### Error: "failed to get LFS files"
**Cause:** Git LFS not initialized or no LFS files
**Fix:** Run `git lfs install` and `git lfs ls-files` to verify

### Error: "failed to register indexd record"
**Cause:** Indexd authentication or permission issue
**Fix:**
1. Check `.drs/config` for correct Gen3 credentials
2. Verify credentials with `git drs list-config`
3. Test access with `curl` to indexd endpoint

### Migration shows "errors > 0"
**Cause:** Individual files failed to migrate
**Fix:**
1. Check migration report for specific errors
2. Common causes: network timeouts, duplicate UUIDs, permission issues
3. Re-run migration (it will skip successfully migrated files)

### Validation shows failures
**Cause:** Indexd records missing or inaccessible
**Fix:**
1. Check validation output for specific files
2. Query indexd directly for those UUIDs
3. Re-run migration for failed files
4. Contact support if persistent

## Safety Features

### No Data Deletion
- Migration only **creates** new records
- Never deletes existing records
- Both v1 and v2 UUIDs coexist

### Idempotent
- Safe to run multiple times
- Skips files already migrated
- No duplicate record creation

### Rollback Capability
- Keep migration reports for audit trail
- Can identify and remove v2 records if needed
- Original v1 records untouched

### Validation
- Built-in validation checks
- Separate validation script for thoroughness
- Continuous monitoring recommended

## Advanced Usage

### Migrate Multiple Projects
```bash
#!/bin/bash
# migrate-all.sh

PROJECTS=(
  "gdc-mirror:/path/to/gdc-mirror"
  "aced-evotypes:/path/to/aced-evotypes"
  "test-data:/path/to/test-data"
)

for project_path in "${PROJECTS[@]}"; do
  project="${project_path%%:*}"
  repo="${project_path#*:}"

  echo "Migrating $project..."
  go run scripts/migrate_uuids.go \
    --project "$project" \
    --repo "$repo" \
    --output "migrations/${project}-$(date +%Y%m%d).json"

  echo "Validating $project..."
  go run scripts/validate_migration.go \
    --project "$project" \
    --mapping "migrations/${project}-$(date +%Y%m%d).json"
done
```

### Export UUID Mapping to CSV
```bash
# Convert JSON report to CSV for Excel
cat migration-report.json | \
  jq -r '.uuid_mappings[] | [.file_path, .legacy_uuid, .new_uuid, .status] | @csv' \
  > uuid-mapping.csv
```

### Check Migration Progress
```bash
# Count migrated files
cat migration-report.json | jq '.migrated'

# List failed files
cat migration-report.json | jq -r '.uuid_mappings[] | select(.status == "error") | .file_path'

# Success rate
cat migration-report.json | jq '.migrated / .total_files * 100'
```

## Best Practices

1. **Always dry-run first** - Test before making changes
2. **Save migration reports** - Keep for audit trail and rollback
3. **Validate thoroughly** - Run validation script after migration
4. **Monitor post-migration** - Watch for download errors
5. **Communicate early** - Notify downstream systems of UUID changes
6. **Migrate incrementally** - Start with small projects, then larger ones
7. **Keep both UUIDs** - Don't rush to deprecate v1 UUIDs

## Support

For questions or issues:
- See main migration plan: `docs/UUID_MIGRATION_PLAN.md`
- File issues: https://github.com/calypr/git-drs/issues
- Contact: #git-drs-support

## Related Documentation

- [UUID Migration Plan](../docs/UUID_MIGRATION_PLAN.md) - Overall strategy
- [UUID Specification](../client/uuid.go) - Technical details
- [Git-DRS Documentation](https://docs.calypr.org/git-drs/) - User guide
