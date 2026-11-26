#!/bin/bash
set -e

GENOMIC_FNA_GZ="$1"
ASSEMBLY_ID="$2"
# Optional: the assembly's name in the JBrowse2 UI ("{version} — {assembly}").
# ASSEMBLY_ID stays its identifier: tracks, sessions and the data file names
# below refer to it, so it must stay shell- and filename-safe.
DISPLAY_LABEL="${3:-}"

if [ -z "$GENOMIC_FNA_GZ" ] || [ -z "$ASSEMBLY_ID" ]; then
  echo "Usage: $0 <genomic_fna.gz> <assembly_id> [display_label]" >&2
  exit 1
fi

TMPDIR=$(mktemp -d -p /jbrowse2-tmp)
trap "rm -rf $TMPDIR" EXIT

echo "Decompressing genomic FASTA..."
gunzip -c "$GENOMIC_FNA_GZ" > "$TMPDIR/${ASSEMBLY_ID}.genomic.fna"

echo "Indexing FASTA..."
samtools faidx "$TMPDIR/${ASSEMBLY_ID}.genomic.fna"

echo "Adding JBrowse2 assembly ${ASSEMBLY_ID}..."
# This is the first script in the pipeline to ever touch /web/data, so guard
# against the setup-jbrowse2-web migrate step ("jbrowse create /web") not
# having populated it yet — exec below would fail with "No such file or
# directory" on a missing parent dir rather than just creating the lock file.
mkdir -p /web/data

# Serialize access to the shared config.json: multiple JBrowse2 setup/track/
# delete scripts can run concurrently (see docker-compose worker replicas),
# and each does a non-atomic read-modify-write of that file.
exec 200>/web/data/.jbrowse-config.lock
flock -x 200
DISPLAY_ARG=()
if [ -n "$DISPLAY_LABEL" ]; then
  DISPLAY_ARG=(--displayName "$DISPLAY_LABEL")
fi
jbrowse add-assembly "$TMPDIR/${ASSEMBLY_ID}.genomic.fna" --name "$ASSEMBLY_ID" "${DISPLAY_ARG[@]}" --load copy --out /web/data --force

echo "JBrowse2 FNA setup complete."
