# Repository line audit contract

This wave is read-only for production source. Workers may write only their assigned TSV report in this directory.

Measure the current repository by responsibility, not by a single `wc -l` total:

1. Separate production Go, Go tests, documentation, configuration, scripts, module metadata, and other assets.
2. Count tracked files separately from untracked worktree files and exclude `.audit/` from product totals.
3. Identify generated files from their source marker rather than their path or size.
4. For each assigned package, state its responsibility in one sentence and classify material files or symbols as core, wrapper, duplicate, dead, test-only, compatibility, or generated.
5. A deletion estimate needs production call-site evidence. Tests alone do not keep production code alive.
6. Do not propose removing compatibility behavior without identifying the persisted format, CLI contract, or wire behavior it protects.

Worker report columns:

`package\tproduction_lines\ttest_lines\tresponsibility\tlarge_files\tdelete_candidates\tmove_or_inline\tcompatibility_constraints\tevidence\tconfidence`
