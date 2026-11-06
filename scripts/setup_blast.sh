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

# makeblastdb's argument parser splits -in at whitespace even when the shell
# passed the path as one quoted argument. Assembly upload folders are named
# after the assembly (e.g. "Lyst 1.0"), so feed makeblastdb a space-free
# symlink instead. Keep the original extension so makeblastdb still detects
# gzip input.
TMP_DIR="$(mktemp -d /tmp/emobase-blast-XXXXXX)"
trap 'rm -rf "$TMP_DIR"' EXIT

EXT=""
case "$FILE_PATH" in
  *.gz) EXT=".gz" ;;
  *.gzip) EXT=".gzip" ;;
esac
TMP_INPUT="$TMP_DIR/input$EXT"
ln -s "$(readlink -f "$FILE_PATH")" "$TMP_INPUT"

makeblastdb \
  -in "$TMP_INPUT" \
  -dbtype "$DB_TYPE" \
  -title "$TITLE" \
  -parse_seqids \
  -out "$OUT"
