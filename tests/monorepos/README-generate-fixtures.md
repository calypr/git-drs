
# monorepo tests

A monorepo on GitHub refers to a single Git repository that contains the source code for multiple distinct projects, libraries, or services. While these projects may be related, they are often logically independent and can be managed by different teams within an organization. 


# Generate Fixtures

Creates directory fixtures from a list of names on stdin. For each input line, the program creates one top-level directory containing 1-6 subdirectories (or a fixed number if specified via flags), each with 100-1000 files (or a fixed number if specified via flags). Progress and errors are printed to `stderr`.

This tool is useful for generating large, deterministic-looking file trees for testing monorepo workflows, Git performance, LFS, CI, etc.

## File

- Program source: `tests/monorepos/generate-fixtures.go`
- Output binary example: `generate-fixtures`

## Defaults

- By default, will generate random file counts based on:
  - Minimum subdirectories: `1`  
  - Maximum subdirectories: `6`  
  - Minimum files per subdirectory: `100`  
  - Maximum files per subdirectory: `1000` 

  However, these constants can be adjusted providing parameters:
  ```--number-of-subdirectories=3 --number-of-files=100```
  See Makefile for example usage.

- File content:
  - Predictable content: file content == relative path
  - Deprecated code will generate random alphanumeric content instead.
  - ~~File size: `1024` bytes (1 KiB)~~  
  - ~~Random content: alphanumeric characters~~

## Safety

- Skips absolute paths.
- Skips paths that would escape the repository root (paths starting with `..` or equal to `..`).
- Prints errors and progress to `stderr` so `stdout` can be piped if needed.

## Build

Run from the repository root:

```bash
# Build & run via Makefile (recommended)
# From tests/monorepos/:
make test-monorepos
# Fixtures are created under `tests/monorepos/fixtures`
```

## Usage

Provide one directory name per line on `stdin`. Example:

```bash
cat <<EOF | ./generate-fixtures
monorepo
service-a
service-b
EOF
```

The program will create `monorepo/`, `service-a/`, `service-b/` with subdirectories and files.

## Integration

- Useful for `make test-monorepos` or CI jobs that need large fixture trees.
- The tool prints per-subdirectory creation progress lines like: `created 123 files in monorepo/sub-directory-1`.

## Exit Codes

- `0` on success (fixtures created).
- Non\-zero on error reading stdin or unexpected filesystem failures.

## Notes

- Intended for local/test environments only; be mindful of disk usage when generating many files.
- Adjust behavior by editing the constants at the top of `tests/monorepos/generate-fixtures.go` and rebuilding.