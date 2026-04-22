#!/bin/bash
set -e

GENOMIC_FNA_GZ="$1"
GENOMIC_GFF_GZ="$2"
VERSION="$3"

if [ -z "$GENOMIC_FNA_GZ" ] || [ -z "$GENOMIC_GFF_GZ" ] || [ -z "$VERSION" ]; then
  echo "Usage: $0 <genomic_fna.gz> <genomic_gff.gz> <version>" >&2
  exit 1
fi

# Use the dedicated image-internal temp directory — not accessible via nginx.
TMPDIR=$(mktemp -d -p /jbrowse2-tmp)
trap "rm -rf $TMPDIR" EXIT

echo "Decompressing input files..."
gunzip -c "$GENOMIC_FNA_GZ" > "$TMPDIR/${VERSION}.genomic.fna"
gunzip -c "$GENOMIC_GFF_GZ" > "$TMPDIR/${VERSION}.genomic.gff"

echo "Indexing FASTA..."
samtools faidx "$TMPDIR/${VERSION}.genomic.fna"

echo "Sorting and compressing GFF..."
jbrowse sort-gff "$TMPDIR/${VERSION}.genomic.gff" | bgzip > "$TMPDIR/${VERSION}.genomic.sorted.gff.gz"
tabix "$TMPDIR/${VERSION}.genomic.sorted.gff.gz"

echo "Adding JBrowse2 assembly and tracks for version ${VERSION}..."
jbrowse add-assembly "$TMPDIR/${VERSION}.genomic.fna" --name "$VERSION" --load copy --out /web/data --force
jbrowse add-track "$TMPDIR/${VERSION}.genomic.sorted.gff.gz" --name "${VERSION} Annotations" --assemblyNames "$VERSION" --load copy --out /web/data --force
jbrowse text-index --out /web/data

echo "JBrowse2 setup complete."
