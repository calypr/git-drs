#!/usr/bin/env bash

set -euo pipefail

repo=${1:?usage: measure-packages.sh REPO_ROOT}

cd "$repo"

printf 'package\tproduction_files\tproduction_loc\tsyfon_client_files\n'

measurements=$(mktemp)
trap 'rm -f "$measurements"' EXIT

while IFS= read -r file; do
  package=$(dirname "$file")
  if [[ "$package" == "." ]]; then
    package=root
  fi
  file_loc=$(wc -l < "$file")
  syfon_file=0
  if rg -q '"github.com/calypr/syfon/client' "$file"; then
    syfon_file=1
  fi
  printf '%s\t%d\t%d\n' "$package" "$file_loc" "$syfon_file" >> "$measurements"
done < <(rg --files -g '*.go' -g '!*_test.go' -g '!tests/**' | sort)

awk -F '\t' '
  { files[$1]++; loc[$1] += $2; syfon[$1] += $3 }
  END {
    for (package in files) {
      printf "%s\t%d\t%d\t%d\n", package, files[package], loc[package], syfon[package]
    }
  }
' "$measurements" | sort
