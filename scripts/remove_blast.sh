#!/bin/bash
set -e
shopt -s nullglob

OUT="$1"

if [ -z "$OUT" ]; then
  echo "Usage: $0 <out>" >&2
  exit 1
fi

files=("${OUT}".*)

if [ ${#files[@]} -eq 0 ]; then
  echo "no blast database files found at ${OUT}, nothing to remove"
  exit 0
fi

rm -f "${files[@]}"
