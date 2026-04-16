#!/bin/bash
set -e

GENOMIC_FNA_GZ="$1"
GENOMIC_GFF_GZ="$2"

if [ -z "$GENOMIC_FNA_GZ" ] || [ -z "$GENOMIC_GFF_GZ" ]; then
  echo "Usage: $0 <genomic_fna.gz> <genomic_gff.gz>" >&2
  exit 1
fi

# Use a subdirectory of the uploads volume as temp space (real disk, not RAM).
TMPDIR=$(mktemp -d -p /app/public/uploads/.tmp)
trap "rm -rf $TMPDIR" EXIT

echo "Decompressing input files..."
gunzip -c "$GENOMIC_FNA_GZ" > "$TMPDIR/genomic.fna"
gunzip -c "$GENOMIC_GFF_GZ" > "$TMPDIR/genomic.gff"

echo "Indexing FASTA..."
samtools faidx "$TMPDIR/genomic.fna"

echo "Sorting and compressing GFF..."
jbrowse sort-gff "$TMPDIR/genomic.gff" | bgzip > "$TMPDIR/genomic.sorted.gff.gz"
tabix "$TMPDIR/genomic.sorted.gff.gz"

echo "Re-creating JBrowse2 tracks..."
rm -rf /web/data/
jbrowse add-assembly "$TMPDIR/genomic.fna" --load copy --out /web/data
jbrowse add-track "$TMPDIR/genomic.sorted.gff.gz" --load copy --out /web/data
jbrowse text-index --out /web/data

echo "JBrowse2 setup complete."
