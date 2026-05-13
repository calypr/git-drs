# Add `-I "pattern"` include filter support to `git drs pull`

## Summary
Add include-pattern filtering to `git drs pull`, similar to legacy `git lfs pull -I "pattern"` workflows.

## Motivation
Current `git drs pull` behavior pulls based on repository resolution without a user-facing path pattern filter. Users migrating from `git lfs pull -I` expect selective hydration of files by glob/path.

## Proposed UX
Support:

```bash
git drs pull -I "results/*.txt"
git drs pull -I "*.bam" -I "data/**"
git drs pull --include "path/to/file"
```

Optional:
- `--exclude` parity (if desired in same change or follow-up)

## Proposed behavior
1. Parse one or more include patterns (`-I`, `--include`).
2. Resolve candidate pointers as usual.
3. Filter by repo-relative path match before download.
4. Download only matched objects; skip others with clear logging.
5. If no pattern supplied, preserve current default behavior.

## Scope
- `cmd/pull/main.go` CLI flags and pull selection pipeline
- pointer/path inventory layer (where path<->OID candidates are produced)
- docs: `docs/commands.md`, `docs/getting-started.md`, `docs/troubleshooting.md`
- tests for include filtering semantics

## Acceptance criteria
- [ ] `git drs pull -I "<pattern>"` works for a single pattern.
- [ ] Repeated `-I` flags are supported.
- [ ] Include matching is against repo-relative paths.
- [ ] Default `git drs pull` behavior unchanged when no `-I` is passed.
- [ ] Help text documents pattern syntax and examples.
- [ ] Unit/integration tests cover positive and negative matches.

## Testing matrix
- Single file exact path include.
- Wildcard include (`*.bam`, `data/**`).
- Multiple `-I` values.
- No matches (should no-op cleanly and return success unless policy says otherwise).
- Mixed matched/unmatched objects in same pull run.

## Notes
This closes a usability gap for users transitioning from `git lfs` CLI habits to `git drs` commands while keeping pull behavior explicit and predictable.

