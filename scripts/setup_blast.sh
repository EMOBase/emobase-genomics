#!/bin/bash
set -eo pipefail

FILE_PATH="$1"
DB_TYPE="$2"
TITLE="$3"
OUT="$4"

if [ -z "$FILE_PATH" ] || [ -z "$DB_TYPE" ] || [ -z "$TITLE" ] || [ -z "$OUT" ]; then
  echo "Usage: $0 <file_path> <db_type> <title> <out>" >&2
  exit 1
fi

# makeblastdb reads -in as a space-separated list of files and detects gzip
# only by a ".gz" suffix. Uploads are always gzip (checked at upload), so
# decompress here and stream the FASTA in on stdin.
gzip -dc -- "$FILE_PATH" | makeblastdb \
  -in - \
  -dbtype "$DB_TYPE" \
  -title "$TITLE" \
  -parse_seqids \
  -out "$OUT"
