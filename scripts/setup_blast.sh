#!/bin/bash
set -e

FILE_PATH="$1"
DB_TYPE="$2"
TITLE="$3"
OUT="$4"

if [ -z "$FILE_PATH" ] || [ -z "$DB_TYPE" ] || [ -z "$TITLE" ] || [ -z "$OUT" ]; then
  echo "Usage: $0 <file_path> <db_type> <title> <out>" >&2
  exit 1
fi

makeblastdb \
  -in "$FILE_PATH" \
  -dbtype "$DB_TYPE" \
  -title "$TITLE" \
  -parse_seqids \
  -out "$OUT"
