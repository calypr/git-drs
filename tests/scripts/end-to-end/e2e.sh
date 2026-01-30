#!/usr/bin/env bash
# Simple e2e test script for testing coverage capture
set -euo pipefail

echo "Running simple e2e tests..."

# Test 1: Check that git-drs exists and is executable
if ! command -v git-drs &> /dev/null; then
    echo "ERROR: git-drs not found in PATH" >&2
    exit 1
fi

echo "✓ git-drs found in PATH"

# Test 2: Run git-drs version command
if git-drs version &> /dev/null; then
    echo "✓ git-drs version command succeeded"
else
    echo "ERROR: git-drs version command failed" >&2
    exit 1
fi

# Test 3: Run git-drs help command
if git-drs --help &> /dev/null; then
    echo "✓ git-drs help command succeeded"
else
    echo "ERROR: git-drs help command failed" >&2
    exit 1
fi

echo "All e2e tests passed!"
