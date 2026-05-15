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

if [ -n "$GENE_ID_KEY" ] && [ -n "$LINK_BASE" ]; then
  echo "Patching config.json with formatDetails for track '${VERSION} Annotations'..."
  JEXL_EXPR="jexl:{emobase_link:'<a href=${LINK_BASE}'+feature.${GENE_ID_KEY}+'>'+feature.${GENE_ID_KEY}+'</a>'}"
  jq --arg name "${VERSION} Annotations" \
     --arg jexl "$JEXL_EXPR" \
     '(.tracks[] | select(.name == $name)) |= . + {formatDetails: {feature: $jexl}}' \
     /web/data/config.json > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json /web/data/config.json
fi

jbrowse text-index --out /web/data

echo "JBrowse2 GFF setup complete."
