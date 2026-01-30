# Coverage Capture and Merge Workflow

This directory contains scripts for capturing and merging code coverage from both unit tests and end-to-end integration tests.

## Overview

The coverage workflow uses Go 1.20+'s `GOCOVERDIR` support to capture raw coverage data from instrumented binaries. This allows teams to:

1. Run unit tests with coverage
2. Run integration/e2e tests with coverage (using instrumented CLI binaries)
3. Merge both coverage datasets into a combined report

## Scripts

### `run-e2e-coverage.sh`

Builds an instrumented `git-drs` binary and runs end-to-end tests while capturing coverage data.

**Usage:**
```bash
./tests/scripts/coverage/run-e2e-coverage.sh
```

**What it does:**
1. Builds `git-drs` with coverage instrumentation (`-cover -covermode=atomic -coverpkg=./...`)
2. Places the binary in `build/coverage/git-drs`
3. Sets `GOCOVERDIR` to capture raw coverage to `coverage/integration/raw/`
4. Runs the e2e test script (`tests/scripts/end-to-end/e2e.sh`)
5. Converts raw coverage to a profile at `coverage/integration/coverage.out`

**Environment Variables:**
- `COVERAGE_ROOT` - Base coverage directory (default: `<repo>/coverage`)
- `INTEGRATION_COV_DIR` - Raw integration coverage directory (default: `<coverage>/integration/raw`)
- `INTEGRATION_PROFILE` - Integration coverage profile output (default: `<coverage>/integration/coverage.out`)
- `BUILD_DIR` - Build directory for instrumented binary (default: `<repo>/build/coverage`)
- `GOFLAGS_EXTRA` - Additional Go build flags

### `combine-coverage.sh`

Merges raw coverage data from unit tests and integration tests into a single combined coverage profile.

**Usage:**
```bash
./tests/scripts/coverage/combine-coverage.sh [unit_dir] [integration_dir] [merged_dir] [output_profile]
```

**Parameters (all optional):**
- `unit_dir` - Unit test raw coverage directory (default: `coverage/unit/raw`)
- `integration_dir` - Integration test raw coverage directory (default: `coverage/integration/raw`)
- `merged_dir` - Output directory for merged raw coverage (default: `coverage/merged/raw`)
- `output_profile` - Output combined coverage profile (default: `coverage/combined.out`)

**What it does:**
1. Validates that both unit and integration raw coverage directories exist
2. Merges raw coverage using `go tool covdata merge`
3. Converts merged coverage to a text profile using `go tool covdata textfmt`

**Environment Variables:**
All parameters can also be set via environment variables:
- `COVERAGE_ROOT` - Base coverage directory
- `UNIT_COV_DIR` - Unit test raw coverage directory
- `INTEGRATION_COV_DIR` - Integration test raw coverage directory
- `MERGED_COV_DIR` - Merged raw coverage directory
- `COMBINED_PROFILE` - Combined coverage profile output

### `assert-coverage-timestamp.sh`

Validates that coverage files are newer than the most recent `.go` source file, ensuring coverage is up-to-date.

## Workflow Example

### Step 1: Run unit tests with raw coverage

```bash
# Create the raw coverage directory
mkdir -p coverage/unit/raw

# Run tests with coverage (use atomic mode to match integration tests)
go test -cover -covermode=atomic ./... -args -test.gocoverdir=$PWD/coverage/unit/raw
```

### Step 2: Run integration tests with coverage

```bash
./tests/scripts/coverage/run-e2e-coverage.sh
```

### Step 3: Combine coverage reports

```bash
./tests/scripts/coverage/combine-coverage.sh
```

### Step 4: View combined coverage

```bash
# Summary view
go tool cover -func=coverage/combined.out

# HTML view
go tool cover -html=coverage/combined.out -o coverage/combined.html
```

## Coverage Modes

**Important:** Both unit and integration tests must use the same coverage mode (e.g., `atomic`). If they use different modes, the merge will fail with a "counter mode clash" error.

The scripts default to `atomic` mode, which is thread-safe and appropriate for concurrent tests.

## Directory Structure

```
coverage/
├── integration/
│   ├── .gitignore       # Ignores raw/ directory
│   ├── raw/             # Raw coverage data from e2e tests (not committed)
│   └── coverage.out     # Integration coverage profile (committed)
├── unit/
│   ├── .gitignore       # Ignores raw/ directory
│   ├── raw/             # Raw coverage data from unit tests (not committed)
│   └── coverage.out     # Unit coverage profile (committed)
├── merged/
│   ├── .gitignore       # Ignores raw/ directory
│   └── raw/             # Merged raw coverage (not committed)
├── combined.out         # Combined coverage profile (committed)
└── combined.html        # Combined coverage HTML report (committed)
```

## Troubleshooting

### "counter mode clash" error

This occurs when unit and integration tests use different coverage modes. Ensure both use the same mode:

```bash
# For unit tests
go test -cover -covermode=atomic ./... -args -test.gocoverdir=$PWD/coverage/unit/raw

# For integration tests (handled by run-e2e-coverage.sh)
go build -cover -covermode=atomic -coverpkg=./... -o build/coverage/git-drs .
```

### "coverage directory not found" error

The raw coverage directories must exist before running tests. Create them with:

```bash
mkdir -p coverage/unit/raw coverage/integration/raw
```

### E2E script not found

The `run-e2e-coverage.sh` script looks for `tests/scripts/end-to-end/e2e.sh` (or `end-2-end/e2e.sh` as fallback). Ensure your e2e test script exists and is executable.

## References

- [Go Coverage Profiling](https://go.dev/blog/cover)
- [Go 1.20 Coverage Improvements](https://go.dev/testing/coverage/)
