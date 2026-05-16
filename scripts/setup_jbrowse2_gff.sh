#!/bin/bash
set -e

GENOMIC_GFF_GZ="$1"
VERSION="$2"
GENE_ID_KEY="${3:-}"
LINK_BASE="${4:-}"

if [ -z "$GENOMIC_GFF_GZ" ] || [ -z "$VERSION" ]; then
  echo "Usage: $0 <genomic_gff.gz> <version> [gene_id_key] [link_base]" >&2
  exit 1
fi

TMPDIR=$(mktemp -d -p /jbrowse2-tmp)
trap "rm -rf $TMPDIR" EXIT

echo "Decompressing genomic GFF..."
gunzip -c "$GENOMIC_GFF_GZ" > "$TMPDIR/${VERSION}.genomic.gff"

echo "Sorting and compressing GFF..."
jbrowse sort-gff "$TMPDIR/${VERSION}.genomic.gff" | bgzip > "$TMPDIR/${VERSION}.genomic.sorted.gff.gz"
tabix "$TMPDIR/${VERSION}.genomic.sorted.gff.gz"

echo "Adding JBrowse2 annotation track and rebuilding text index for version ${VERSION}..."
jbrowse add-track "$TMPDIR/${VERSION}.genomic.sorted.gff.gz" --name "${VERSION} Annotations" --assemblyNames "$VERSION" --load copy --out /web/data --force

# Run text-index before the jq patch: text-index rewrites config.json and
# would overwrite any formatDetails we inject before it runs.
jbrowse text-index --out /web/data

echo "JBrowse2 GFF setup complete."
