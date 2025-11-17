# Git-DRS UUID Migration

Quick guide for migrating projects from old project-based UUIDs to new path-based UUIDs.

## What Changed

**Old UUID format:** Based on project ID + SHA256  
**New UUID format:** Based on file path + SHA256 + size

Both UUIDs work during migration - old ones keep working, new ones get created.

## Migration Steps

### 1. Test Run (Dry Run)
```bash
go run scripts/migrate_uuids.go \
  --project your-project \
  --repo /path/to/repo \
  --dry-run \
  --output test-report.json
```

Check `test-report.json` - make sure file count looks right.

### 2. Run Migration
```bash
go run scripts/migrate_uuids.go \
  --project your-project \
  --repo /path/to/repo \
  --output migration-report.json
```

**Save the report file** - you'll need it if anything breaks.

### 3. Validate It Worked
```bash
go run scripts/validate_migration.go \
  --project your-project \
  --mapping migration-report.json
```

Should say "PASSED". If not, check what failed and re-run step 2.

### 4. Update Your Systems

If you reference UUIDs anywhere (databases, configs, etc), update them:

```bash
# Get the mapping as CSV for easy viewing
cat migration-report.json | jq -r '.uuid_mappings[] | [.file_path, .legacy_uuid, .new_uuid] | @csv' > mapping.csv
```

Then update your systems using the old → new UUID mappings.

## Common Issues

**"project ID is required"**  
→ Add `--project your-project-name`

**"not a git repository"**  
→ Check the path is correct: `ls /path/to/repo/.git`

**"failed to register"**  
→ Check your `.drs/config` has valid Gen3 credentials

**Some files failed**  
→ Just re-run the migration, it'll skip the ones that worked

## Scripts

- `migrate_uuids.go` - Creates new UUID records
- `validate_migration.go` - Checks migration worked

## Safety

- Old UUIDs keep working
- Safe to run multiple times
- Only creates new records, never deletes
