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

echo "Adding JBrowse2 track..."
jbrowse add-track "$TMPDIR/$BASENAME" \
  --name "$TRACK_NAME" \
  --assemblyNames "$ASSEMBLY_NAME" \
  --trackId "$TRACK_ID" \
  --load copy \
  --out /web/data \
  --force

echo "JBrowse2 track added successfully."
