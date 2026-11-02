#!/usr/bin/env python3
"""Measure exact-path pull discovery in a repository with many LFS files."""

import argparse
import hashlib
import json
import statistics
import subprocess
import tempfile
import time
from pathlib import Path


def run(*args: str, cwd: Path) -> None:
    subprocess.run(args, cwd=cwd, check=True, stdout=subprocess.DEVNULL)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--files", type=int, default=300)
    parser.add_argument("--samples", type=int, default=5)
    parser.add_argument("--hydrated", action="store_true")
    args = parser.parse_args()

    binary = args.binary.resolve()
    payload = b"hydrated payload"
    oid = hashlib.sha256(payload).hexdigest()
    pointer = (
        "version https://git-lfs.github.com/spec/v1\n"
        f"oid sha256:{oid}\n"
        f"size {len(payload)}\n"
    ).encode()
    with tempfile.TemporaryDirectory(prefix="git-drs-pull-bench-") as name:
        repo = Path(name)
        run("git", "init", "-q", cwd=repo)
        run("git", "config", "filter.drs.clean", "cat", cwd=repo)
        (repo / ".gitattributes").write_text("*.bin filter=drs -text\n")
        for i in range(args.files):
            (repo / f"file-{i:05d}.bin").write_bytes(pointer)
        run("git", "add", ".", cwd=repo)
        if args.hydrated:
            for i in range(args.files):
                (repo / f"file-{i:05d}.bin").write_bytes(payload)

        selected = f"file-{args.files - 1:05d}.bin"
        timings = []
        for _ in range(args.samples):
            start = time.perf_counter()
            result = subprocess.run(
                [str(binary), "pull", "--dry-run", "-I", selected],
                cwd=repo,
                check=True,
                capture_output=True,
                text=True,
            )
            timings.append(time.perf_counter() - start)
            if result.stdout.strip() != selected:
                raise RuntimeError(f"unexpected selected path: {result.stdout!r}")
        print(json.dumps({
            "files": args.files,
            "hydrated": args.hydrated,
            "samples_s": timings,
            "median_s": statistics.median(timings),
        }))


if __name__ == "__main__":
    main()
