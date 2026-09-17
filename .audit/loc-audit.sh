#!/usr/bin/env bash
set -euo pipefail

mode="${1:-tracked}"
case "$mode" in
  tracked)
    list_args=(--cached)
    ;;
  worktree)
    list_args=(--cached --others --exclude-standard)
    ;;
  *)
    echo "usage: $0 [tracked|worktree]" >&2
    exit 2
    ;;
esac

printf 'path\tkind\ttop\tpackage\tlines\tbytes\tgenerated\n'
while IFS= read -r -d '' file; do
  case "$file" in
    .audit/*) continue ;;
  esac
  [[ -f "$file" ]] || continue

  lines=$(awk 'END { print NR + 0 }' "$file")
  bytes=$(wc -c < "$file" | tr -d '[:space:]')
  top=${file%%/*}
  if [[ "$top" == "$file" ]]; then
    top='.'
  fi
  package=$(dirname "$file")
  generated=false
  kind=other

  case "$file" in
    *.go)
      if sed -n '1,20p' "$file" | rg -q 'Code generated .* DO NOT EDIT'; then
        generated=true
      fi
      if [[ "$file" == *_test.go ]]; then
        kind=go_test
      else
        kind=go_prod
      fi
      ;;
    *.md) kind=docs ;;
    *.sh) kind=shell ;;
    *.yml|*.yaml|*.json|*.toml|*.ini) kind=config ;;
    go.mod|go.sum) kind=module ;;
  esac

  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$file" "$kind" "$top" "$package" "$lines" "$bytes" "$generated"
done < <(git ls-files -z "${list_args[@]}")
