#!/bin/bash
set -e

TRACK_GZ="$1"
TRACK_NAME="$2"
ASSEMBLY_NAME="$3"
TRACK_ID="$4"

if [ -z "$TRACK_GZ" ] || [ -z "$TRACK_NAME" ] || [ -z "$ASSEMBLY_NAME" ] || [ -z "$TRACK_ID" ]; then
  echo "Usage: $0 <track.gz> <track_name> <assembly_name> <track_id>" >&2
  exit 1
fi

TMPDIR=$(mktemp -d -p /jbrowse2-tmp)
trap "rm -rf $TMPDIR" EXIT

BASENAME=$(basename "$TRACK_GZ")
BASENAME="${BASENAME%.gzip}"
BASENAME="${BASENAME%.gz}"

echo "Decompressing track file..."
gunzip -c "$TRACK_GZ" > "$TMPDIR/$BASENAME"

# GFF files must be sorted, bgzip-compressed, and tabix-indexed before
# jbrowse add-track can register them correctly.
case "$BASENAME" in
  *.gff|*.gff3)
    SORTED_FILE="$TMPDIR/${BASENAME%.*}.sorted.gff.gz"
    echo "Sorting and compressing GFF..."
    jbrowse sort-gff "$TMPDIR/$BASENAME" | bgzip > "$SORTED_FILE"
    tabix "$SORTED_FILE"
    TRACK_FILE="$SORTED_FILE"
    ;;
  *)
    TRACK_FILE="$TMPDIR/$BASENAME"
    ;;
esac

echo "Adding JBrowse2 track..."
jbrowse add-track "$TRACK_FILE" \
  --name "$TRACK_NAME" \
  --assemblyNames "$ASSEMBLY_NAME" \
  --trackId "$TRACK_ID" \
  --load copy \
  --out /web/data \
  --force

echo "JBrowse2 track added successfully."
