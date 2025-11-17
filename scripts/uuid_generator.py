#!/usr/bin/env python3
"""
Git-DRS Deterministic UUID Generator (Python Reference Implementation)

This script demonstrates how external tools (Forge, g3t_etl, etc.) can generate
the same deterministic UUIDs as Git-DRS without depending on Git-DRS itself.

Specification:
    canonical = f"did:gen3:{authority}:{normalized_path}:{sha256_lowercase}:{size}"
    uuid = UUIDv5(namespace, canonical)

    Where:
    - authority = "calypr.org" (constant)
    - namespace = UUIDv3(DNS_NAMESPACE, "aced-idp.org")
    - normalized_path = POSIX-style path with leading slash
    - sha256_lowercase = lowercase hex digest (64 chars)
    - size = integer bytes

Usage:
    python scripts/uuid_generator.py /path/to/file.fastq abc123... 1024000
    python scripts/uuid_generator.py --help
"""

import uuid
import sys
import re
import argparse
from typing import Tuple


# Constants matching Go implementation
AUTHORITY = "calypr.org"
ACED_NAMESPACE = uuid.uuid3(uuid.NAMESPACE_DNS, 'aced-idp.org')


def normalize_logical_path(path: str) -> str:
    """
    Normalize a file path to ensure consistent UUID generation.

    Rules:
    - Convert backslashes to forward slashes
    - Remove duplicate slashes
    - Remove trailing slash (unless root)
    - Ensure leading slash

    Args:
        path: File path to normalize

    Returns:
        Normalized path string

    Examples:
        >>> normalize_logical_path("data/sample.fastq")
        '/data/sample.fastq'
        >>> normalize_logical_path("/data//sample.fastq/")
        '/data/sample.fastq'
        >>> normalize_logical_path("data\\\\sample.fastq")
        '/data/sample.fastq'
    """
    # Convert backslashes to forward slashes
    path = path.replace('\\', '/')

    # Remove duplicate slashes
    path = re.sub(r'/+', '/', path)

    # Remove trailing slash unless root
    if len(path) > 1 and path.endswith('/'):
        path = path.rstrip('/')

    # Ensure leading slash
    if not path.startswith('/'):
        path = '/' + path

    return path


def compute_deterministic_uuid(logical_path: str, sha256: str, size: int) -> str:
    """
    Generate a deterministic UUID based on the canonical DID string.

    This matches the Git-DRS Go implementation exactly.

    Args:
        logical_path: Repository-relative path to the file
        sha256: SHA256 hash of the file (hex string)
        size: Size of the file in bytes

    Returns:
        Deterministic UUID string (with hyphens)

    Examples:
        >>> compute_deterministic_uuid(
        ...     "/projectA/raw/reads/R1.fastq.gz",
        ...     "4d9670e4c8f3e8b8a6c2d4f9136d7b89e4b9d5e0d2a1c0b9f4c2de0e8c7ac1a0",
        ...     382991274
        ... )
        'd61939fc-2919-511f-88f6-3d2d8566f5a4'
    """
    # Normalize inputs
    normalized_path = normalize_logical_path(logical_path)
    normalized_sha256 = sha256.lower()

    # Build canonical DID string
    canonical = f"did:gen3:{AUTHORITY}:{normalized_path}:{normalized_sha256}:{size}"

    # Generate UUIDv5 (SHA1-based)
    deterministic_uuid = uuid.uuid5(ACED_NAMESPACE, canonical)

    return str(deterministic_uuid)


def validate_inputs(sha256: str, size: int) -> Tuple[bool, str]:
    """
    Validate input parameters.

    Args:
        sha256: SHA256 hash string
        size: File size in bytes

    Returns:
        Tuple of (is_valid, error_message)
    """
    # Validate SHA256
    if len(sha256) != 64:
        return False, f"SHA256 must be 64 characters, got {len(sha256)}"

    if not re.match(r'^[0-9a-fA-F]{64}$', sha256):
        return False, "SHA256 must be hexadecimal"

    # Validate size
    if size < 0:
        return False, "Size must be non-negative"

    return True, ""


def main():
    parser = argparse.ArgumentParser(
        description='Generate deterministic UUIDs for Git-DRS files',
        epilog='Example: %(prog)s /data/file.bam abc123...def 1024000'
    )
    parser.add_argument(
        'path',
        help='File path (will be normalized)'
    )
    parser.add_argument(
        'sha256',
        help='SHA256 hash (64-character hex string)'
    )
    parser.add_argument(
        'size',
        type=int,
        help='File size in bytes'
    )
    parser.add_argument(
        '--show-canonical',
        action='store_true',
        help='Show canonical DID string'
    )
    parser.add_argument(
        '--show-namespace',
        action='store_true',
        help='Show namespace UUID'
    )
    parser.add_argument(
        '--verify',
        help='Verify against expected UUID'
    )

    args = parser.parse_args()

    # Validate inputs
    is_valid, error_msg = validate_inputs(args.sha256, args.size)
    if not is_valid:
        print(f"Error: {error_msg}", file=sys.stderr)
        sys.exit(1)

    # Compute UUID
    generated_uuid = compute_deterministic_uuid(args.path, args.sha256, args.size)

    # Output
    if args.show_canonical:
        normalized_path = normalize_logical_path(args.path)
        canonical = f"did:gen3:{AUTHORITY}:{normalized_path}:{args.sha256.lower()}:{args.size}"
        print(f"Canonical DID: {canonical}")

    if args.show_namespace:
        print(f"Namespace UUID: {ACED_NAMESPACE}")

    print(f"Generated UUID: {generated_uuid}")

    # Verify if requested
    if args.verify:
        if generated_uuid == args.verify:
            print("✓ UUID matches expected value")
            sys.exit(0)
        else:
            print(f"✗ UUID mismatch!")
            print(f"  Expected: {args.verify}")
            print(f"  Got:      {generated_uuid}")
            sys.exit(1)


def test_cases():
    """Run test cases to verify implementation matches Go version."""
    print("Running test cases...")

    # Test 1: Basic UUID generation
    uuid1 = compute_deterministic_uuid(
        "/data/sample.fastq",
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        1024000
    )
    print(f"Test 1: {uuid1}")

    # Test 2: Path normalization
    uuid2a = compute_deterministic_uuid(
        "data/sample.fastq",
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        1024000
    )
    uuid2b = compute_deterministic_uuid(
        "/data/sample.fastq",
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        1024000
    )
    assert uuid2a == uuid2b, "Path normalization failed"
    print(f"Test 2: Path normalization works ✓")

    # Test 3: Case insensitive hash
    uuid3a = compute_deterministic_uuid(
        "/data/sample.fastq",
        "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855",
        1024000
    )
    uuid3b = compute_deterministic_uuid(
        "/data/sample.fastq",
        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        1024000
    )
    assert uuid3a == uuid3b, "Case sensitivity failed"
    print(f"Test 3: Case insensitive hash works ✓")

    # Test 4: From specification example
    uuid4 = compute_deterministic_uuid(
        "/projectA/raw/reads/R1.fastq.gz",
        "4d9670e4c8f3e8b8a6c2d4f9136d7b89e4b9d5e0d2a1c0b9f4c2de0e8c7ac1a0",
        382991274
    )
    print(f"Test 4 (spec example): {uuid4}")

    # Test 5: Namespace UUID
    print(f"Test 5: ACED_NAMESPACE = {ACED_NAMESPACE}")
    expected_namespace = "3dbb886f-620b-3c52-bcb1-1992e7c6ccd5"
    assert str(ACED_NAMESPACE) == expected_namespace, f"Namespace mismatch: expected {expected_namespace}, got {ACED_NAMESPACE}"
    print(f"Test 5: Namespace UUID matches Go implementation ✓")

    print("\nAll tests passed! ✓")


if __name__ == '__main__':
    # If no arguments, run tests
    if len(sys.argv) == 1:
        test_cases()
    else:
        main()
