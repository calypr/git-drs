# Git-DRS UUID Migration Plan: Dual-UUID Period Strategy

## Executive Summary

This document outlines the migration strategy from the legacy UUID scheme (project-based) to the new deterministic UUID scheme (path-based) for Git-DRS. The migration uses a dual-UUID period approach to ensure zero downtime and maintain backward compatibility.

**Timeline:** 6-month transition period
**Risk Level:** Low (dual-UUID support maintains full backward compatibility)
**Impact:** Existing projects continue working; new projects use new scheme automatically

---

## Background

### Legacy UUID Scheme (v1)
```go
// Old method
uuid = UUIDv5(namespace, "repoName:sha256")
```
- **Pros:** Simple, worked for initial use cases
- **Cons:** Not path-aware, requires knowing repoName, can't be generated independently

### New UUID Scheme (v2)
```go
// New method
canonical = "did:gen3:calypr.org:{path}:{sha256}:{size}"
uuid = UUIDv5(ACED_NAMESPACE, canonical)
```
- **Pros:** Path-aware, deterministic, independent generation, no server access needed
- **Cons:** Existing UUIDs incompatible, requires migration

---

## Migration Strategy: Dual-UUID Period

### Phase 1: Preparation (Weeks 1-2)

**Goal:** Deploy backward-compatible code that supports both UUID schemes

**Tasks:**
- [x] Implement new UUID generation functions
- [x] Create comprehensive tests
- [ ] Add backward compatibility layer to download/lookup functions
- [ ] Create migration scripts
- [ ] Set up monitoring and validation tools

**Deliverables:**
- Backward-compatible Git-DRS release
- Migration scripts for existing projects
- Validation test suite

### Phase 2: Deployment (Week 3)

**Goal:** Deploy new Git-DRS version with dual-UUID support

**Tasks:**
- [ ] Deploy new version to staging environment
- [ ] Run validation tests against existing projects (gdc-mirror, aced-evotypes)
- [ ] Verify backward compatibility
- [ ] Deploy to production
- [ ] Monitor for errors

**Success Criteria:**
- All existing files downloadable via legacy UUIDs
- New commits generate v2 UUIDs
- No download failures
- Zero data loss

### Phase 3: Migration (Weeks 4-16)

**Goal:** Gradually migrate existing projects to new UUID scheme

**Migration Priority:**
1. **High:** Active projects with ongoing updates
2. **Medium:** Stable projects with occasional updates
3. **Low:** Archived projects (read-only)

**Per-Project Migration Steps:**
1. **Analysis:** Run analysis script to inventory existing files
2. **Dry Run:** Test migration on clone without committing
3. **Migration:** Create new indexd records with v2 UUIDs
4. **Validation:** Verify all files downloadable via both UUIDs
5. **Documentation:** Export UUID mapping for external systems
6. **Notification:** Inform downstream consumers (Forge, g3t_etl, etc.)

**Weekly Cadence:**
- Week 4-8: Migrate 2-3 active projects per week
- Week 9-12: Migrate remaining active projects
- Week 13-16: Migrate stable/archived projects

### Phase 4: Transition (Weeks 17-20)

**Goal:** Update external systems to use v2 UUIDs

**Tasks:**
- [ ] Update Forge to use deterministic UUID generation
- [ ] Update g3t_etl to use v2 UUIDs
- [ ] Update documentation and examples
- [ ] Add deprecation warnings for v1 UUID lookups

**Deliverables:**
- Updated external tools
- Migration guides for users
- Deprecation notices in logs

### Phase 5: Cleanup (Weeks 21-26)

**Goal:** Deprecate legacy UUID support (optional)

**Tasks:**
- [ ] Monitor v1 UUID lookup usage (should be near zero)
- [ ] Decision point: Keep dual support or deprecate v1
- [ ] If deprecating: Add stronger warnings
- [ ] If deprecating: Set end-of-life date (e.g., +6 months)

**Options:**
- **Option A (Recommended):** Maintain dual-UUID support indefinitely (low cost, high safety)
- **Option B:** Deprecate v1 after confirming <1% usage
- **Option C:** Hard cutover after all projects migrated

---

## Technical Implementation

### 1. Backward-Compatible Download

**Current:** Downloads query by SHA256, find project-matching record
**Enhancement:** Support both UUID lookup paths

```go
// In indexd.go: GetDownloadURL()
func (cl *IndexDClient) GetDownloadURL(oid string) (*drs.AccessURL, error) {
    // Get all records matching the SHA256
    records, err := cl.GetObjectsByHash(drs.ChecksumTypeSHA256.String(), oid)
    if err != nil {
        return nil, err
    }

    // Find matching record (works for both v1 and v2 UUIDs)
    matchingRecord, err := FindMatchingRecord(records, cl.ProjectId)
    if err != nil {
        return nil, err
    }

    if matchingRecord == nil {
        return nil, fmt.Errorf("no DRS object found for OID %s", oid)
    }

    // Get DRS object and download URL
    drsObj, err := cl.GetObject(matchingRecord.Did)
    // ... rest of function
}
```

**Key Point:** No changes needed! The current implementation already queries by SHA256, so it finds records regardless of UUID version.

### 2. Migration Script

**Location:** `scripts/migrate_uuids.go`

```go
// Usage: go run scripts/migrate_uuids.go --project gdc-mirror --repo /path/to/repo --dry-run
```

**Features:**
- Dry-run mode for safe testing
- Progress tracking and logging
- UUID mapping export (JSON/CSV)
- Rollback capability
- Validation checks

### 3. Indexd Record Enhancement

**Add migration metadata to indexd records:**

```json
{
  "did": "new-v2-uuid",
  "hashes": {"sha256": "abc123..."},
  "size": 12345,
  "urls": ["s3://bucket/path"],
  "metadata": {
    "uuid_version": "v2",
    "legacy_uuid": "old-v1-uuid",
    "migration_date": "2024-01-15T10:30:00Z",
    "canonical_path": "/data/sample.fastq"
  }
}
```

**Benefits:**
- Enables UUID lookup by legacy ID
- Tracks migration status
- Supports bidirectional mapping
- Preserves audit trail

---

## Migration Scripts

### Script 1: Analyze Project

**Purpose:** Inventory existing files and estimate migration scope

```bash
go run scripts/analyze_project.go --project gdc-mirror --repo /path/to/repo

# Output:
# Total LFS files: 1,234
# Unique SHA256 hashes: 1,100
# Existing indexd records: 1,050
# Records needing migration: 1,050
# Estimated indexd records after migration: 1,234 (one per path)
```

### Script 2: Migrate Project (Dry Run)

```bash
go run scripts/migrate_uuids.go \
  --project gdc-mirror \
  --repo /path/to/repo \
  --dry-run \
  --output migration-plan.json

# Validates migration without changing indexd
```

### Script 3: Execute Migration

```bash
go run scripts/migrate_uuids.go \
  --project gdc-mirror \
  --repo /path/to/repo \
  --output uuid-mapping.json

# Creates new indexd records, preserves old ones
```

### Script 4: Validate Migration

```bash
go run scripts/validate_migration.go \
  --project gdc-mirror \
  --mapping uuid-mapping.json

# Tests:
# - All files downloadable via legacy UUIDs
# - All files downloadable via new UUIDs
# - No orphaned records
# - No missing files
```

---

## Risk Mitigation

### Risk 1: Download Failures During Migration
**Likelihood:** Low
**Impact:** High
**Mitigation:**
- Dual-UUID support ensures old UUIDs continue working
- Create new records before deprecating old ones
- Rollback capability in migration script

### Risk 2: External System Breakage
**Likelihood:** Medium
**Impact:** Medium
**Mitigation:**
- Export UUID mappings for all migrated projects
- Provide transition period with warnings
- Maintain legacy_uuid metadata field

### Risk 3: Indexd Record Proliferation
**Likelihood:** Certain
**Impact:** Low
**Mitigation:**
- Indexd designed for scale (millions of records)
- Duplicate files share same S3 storage (no cost increase)
- Can mark old records as deprecated without deleting

### Risk 4: Incomplete Migration
**Likelihood:** Low
**Impact:** Medium
**Mitigation:**
- Automated validation scripts
- Per-project migration checklist
- Monitor download success rates

---

## Validation Strategy

### Pre-Migration Validation
- [ ] All existing files downloadable via current Git-DRS
- [ ] Indexd records complete and accessible
- [ ] Backup of indexd records created

### Post-Migration Validation
- [ ] All files downloadable via legacy UUIDs
- [ ] All files downloadable via new UUIDs
- [ ] UUID mapping exported and verified
- [ ] No download errors in monitoring
- [ ] External systems updated and tested

### Continuous Monitoring
- **Metric:** Download success rate (target: >99.9%)
- **Metric:** Legacy UUID lookup count (should decline over time)
- **Metric:** New UUID adoption rate (should increase to 100%)
- **Alert:** Any download failures trigger investigation

---

## Communication Plan

### Week 1: Internal Announcement
**Audience:** Development team
**Message:** New UUID scheme deployed, backward compatible, no action needed for existing projects

### Week 3: User Notification
**Audience:** Active Git-DRS users
**Message:**
- New UUID scheme available for better path tracking
- Existing projects continue working
- Migration optional but recommended
- Migration support available

### Week 4+: Per-Project Communication
**Audience:** Project owners
**Message:**
- Your project scheduled for migration (Week X)
- What to expect: brief read-only period, validation tests
- Action needed: Update external systems using UUID mapping
- Support available for questions

### Week 17: External System Notification
**Audience:** Forge, g3t_etl, portal developers
**Message:**
- All projects migrated to v2 UUIDs
- UUID mappings available
- Please update to use deterministic UUID generation
- Legacy support available for 6 months

---

## Rollback Procedures

### If Migration Fails for a Project

1. **Stop migration script**
2. **Verify no indexd records deleted** (migration is additive only)
3. **Validate old UUIDs still work**
4. **Analyze failure logs**
5. **Fix issue and retry with dry-run**

### If Download Failures Occur

1. **Identify affected files** (monitoring alerts)
2. **Check indexd for missing records**
3. **Restore from backup if needed**
4. **Re-run migration for affected files**

### Emergency Rollback (Worst Case)

1. **Revert Git-DRS to previous version** (v1 UUID only)
2. **Delete new v2 indexd records** (keep legacy records)
3. **Communicate incident to users**
4. **Root cause analysis**
5. **Fix and retry migration with additional safeguards**

**Rollback Time:** <1 hour (revert deployment)
**Data Loss Risk:** Zero (no deletions, only additions)

---

## Success Criteria

### Technical Success
- [x] New UUID generation functions implemented and tested
- [ ] 100% of existing files downloadable during transition
- [ ] All active projects successfully migrated
- [ ] Zero data loss or corruption
- [ ] External systems updated and functional

### Business Success
- [ ] User satisfaction maintained (no complaints about data access)
- [ ] Migration completed within 6-month timeline
- [ ] Documentation updated and clear
- [ ] Team trained on new UUID scheme

### Long-term Success
- [ ] New projects automatically use v2 UUIDs
- [ ] Independent metadata generation working (Forge, etc.)
- [ ] Commits no longer require server access
- [ ] Path-aware file tracking enables better data provenance

---

## Timeline Summary

| Phase | Duration | Key Milestone |
|-------|----------|---------------|
| **Preparation** | Weeks 1-2 | Deploy backward-compatible code |
| **Deployment** | Week 3 | Production deployment with dual-UUID support |
| **Migration** | Weeks 4-16 | Migrate all existing projects |
| **Transition** | Weeks 17-20 | Update external systems |
| **Cleanup** | Weeks 21-26 | Monitor and optionally deprecate v1 |

**Total Duration:** 6 months (26 weeks)

---

## Next Steps

### Immediate (This Week)
1. ✅ Implement deterministic UUID generation
2. ✅ Create comprehensive tests
3. [ ] Review and approve this migration plan
4. [ ] Create migration scripts
5. [ ] Set up monitoring infrastructure

### Short-term (Next 2 Weeks)
1. [ ] Deploy to staging environment
2. [ ] Test migration on pilot project (small, low-risk)
3. [ ] Create user documentation
4. [ ] Prepare communication materials

### Medium-term (Month 2-4)
1. [ ] Migrate active projects (gdc-mirror, aced-evotypes)
2. [ ] Export UUID mappings
3. [ ] Update external systems
4. [ ] Monitor and validate

### Long-term (Month 5-6)
1. [ ] Complete remaining migrations
2. [ ] Evaluate v1 deprecation
3. [ ] Finalize documentation
4. [ ] Conduct retrospective

---

## Appendix

### A. Projects to Migrate

| Project | Priority | Estimated Files | Migration Week |
|---------|----------|-----------------|----------------|
| gdc-mirror | High | ~5,000 | Week 4 |
| aced-evotypes | High | ~2,000 | Week 5 |
| test-project-1 | Medium | ~100 | Week 8 |
| archived-data | Low | ~10,000 | Week 14 |

### B. External Systems Affected

1. **Forge (FHIR Metadata Generator)**
   - Impact: DocumentReference.id references
   - Action: Update to use deterministic UUID function
   - Timeline: Week 17

2. **g3t_etl (ETL Pipeline)**
   - Impact: Database foreign keys
   - Action: Add legacy_uuid column, update queries
   - Timeline: Week 18

3. **BForePC/Explorer (Data Portal)**
   - Impact: DRS links, bookmarks
   - Action: Implement UUID redirect service
   - Timeline: Week 19

### C. Migration Script Reference

See `scripts/migrate_uuids.go` for full implementation.

### D. Validation Checklist

- [ ] Pre-migration backup created
- [ ] Dry-run completed successfully
- [ ] Migration executed
- [ ] All files downloadable (legacy UUIDs)
- [ ] All files downloadable (new UUIDs)
- [ ] UUID mapping exported
- [ ] External systems notified
- [ ] Monitoring shows no errors
- [ ] Project marked as migrated

---

## Contact & Support

**Migration Lead:** TBD
**Technical Support:** #git-drs-support
**Documentation:** https://docs.calypr.org/git-drs/uuid-migration
**Issue Tracker:** https://github.com/calypr/git-drs/issues

---

**Document Version:** 1.0
**Last Updated:** 2024-01-15
**Next Review:** 2024-02-01
