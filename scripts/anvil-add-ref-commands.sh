#!/usr/bin/env bash
set -euo pipefail

usage() {
    cat <<'EOF'
Generate git-drs add-ref commands from an AnVIL TSV export.

Usage:
    anvil-add-ref-commands.sh <input.tsv>

The input must contain columns named "files.drs_uri" and "files.file_name".
Commands are written to standard output; they are not executed.
EOF
}

die() {
    printf 'error: %s\n' "$*" >&2
    exit 1
}

split_tsv_line() {
    local line=$1

    # macOS ships Bash 3.2, which does not support namerefs. Return fields
    # through this global array instead so the script works with both
    # the system Bash on macOS and newer Bash releases.
    tsv_fields=()
    while [[ $line == *$'\t'* ]]; do
        tsv_fields+=("${line%%$'\t'*}")
        line=${line#*$'\t'}
    done
    tsv_fields+=("$line")
}

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
    usage
    exit 0
fi

[[ $# -eq 1 ]] || {
    usage >&2
    exit 2
}

input=$1
[[ -f $input ]] || die "TSV file not found: $input"
[[ -r $input ]] || die "TSV file is not readable: $input"

IFS= read -r header_line < "$input" || die "TSV file is empty: $input"
header_line=${header_line%$'\r'}
header_line=${header_line#$'\xef\xbb\xbf'}

declare -a headers
declare -a tsv_fields
split_tsv_line "$header_line"
headers=("${tsv_fields[@]}")

drs_uri_index=-1
file_name_index=-1
for index in "${!headers[@]}"; do
    case ${headers[$index]} in
        files.drs_uri) drs_uri_index=$index ;;
        files.file_name) file_name_index=$index ;;
    esac
done

((drs_uri_index >= 0)) || die 'required column not found: files.drs_uri'
((file_name_index >= 0)) || die 'required column not found: files.file_name'

line_number=1
while IFS= read -r line || [[ -n $line ]]; do
    ((line_number += 1))
    line=${line%$'\r'}
    [[ -n $line ]] || continue

    declare -a fields
    split_tsv_line "$line"
    fields=("${tsv_fields[@]}")

    drs_uri=${fields[$drs_uri_index]:-}
    file_name=${fields[$file_name_index]:-}
    [[ -n $drs_uri ]] || die "line $line_number has an empty files.drs_uri value"
    [[ -n $file_name ]] || die "line $line_number has an empty files.file_name value"

    printf 'git drs add-ref --remote anvil %q %q\n' "$drs_uri" "$file_name"
done < <(tail -n +2 -- "$input")
