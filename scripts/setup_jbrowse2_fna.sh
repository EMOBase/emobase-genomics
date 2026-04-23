#!/bin/bash
set -e

GENOMIC_FNA_GZ="$1"
VERSION="$2"

if [ -z "$GENOMIC_FNA_GZ" ] || [ -z "$VERSION" ]; then
  echo "Usage: $0 <genomic_fna.gz> <version>" >&2
  exit 1
fi

TMPDIR=$(mktemp -d -p /jbrowse2-tmp)
trap "rm -rf $TMPDIR" EXIT

echo "Decompressing genomic FASTA..."
gunzip -c "$GENOMIC_FNA_GZ" > "$TMPDIR/${VERSION}.genomic.fna"

echo "Indexing FASTA..."
samtools faidx "$TMPDIR/${VERSION}.genomic.fna"

echo "Adding JBrowse2 assembly for version ${VERSION}..."
jbrowse add-assembly "$TMPDIR/${VERSION}.genomic.fna" --name "$VERSION" --load copy --out /web/data --force

echo "JBrowse2 FNA setup complete."
