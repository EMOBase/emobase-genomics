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

if [ -n "$GENE_ID_KEY" ] && [ -n "$LINK_BASE" ]; then
  echo "Patching config.json with formatDetails for track '${VERSION} Annotations'..."

  if echo "$GENE_ID_KEY" | grep -q '\.'; then
    ATTR_KEY="${GENE_ID_KEY%%.*}"
    DB_NAME="${GENE_ID_KEY#*.}"
    # JBrowse2 lowercases feature attribute names; DB_NAME stays as-is since
    # it matches against the string content of the attribute value.
    # feature.dbxref can be a string (single value) or array (multiple values);
    # '' + feature.dbxref normalises both to a comma-separated string.
    # GUARD: short-circuits when the attribute is absent (non-gene features)
    # or when DB_NAME prefix is not found (split()[1] would be undefined).
    # EXTRACT: safe to evaluate when GUARD is truthy.
    # One clean ternary avoids nested-ternary precedence pitfalls.
    GUARD="feature.${ATTR_KEY,,} && split('' + feature.${ATTR_KEY,,},'${DB_NAME}:')[1]"
    EXTRACT="split(split('' + feature.${ATTR_KEY,,},'${DB_NAME}:')[1],',')[0]"
    JEXL_EXPR="jexl:{emobase_link:${GUARD} ? '<a href=${LINK_BASE}'+${EXTRACT}+'>'+${EXTRACT}+'</a>' : ''}"
  else
    ID_EXPR="feature.${GENE_ID_KEY,,}"
    JEXL_EXPR="jexl:{emobase_link:${ID_EXPR} ? '<a href=${LINK_BASE}'+${ID_EXPR}+'>'+${ID_EXPR}+'</a>' : ''}"
  fi

  MATCHED=$(jq --arg name "${VERSION} Annotations" '[.tracks[] | select(.name == $name)] | length' /web/data/config.json)
  if [ "$MATCHED" -eq 0 ]; then
    echo "WARNING: no track named '${VERSION} Annotations' found in config.json — formatDetails not injected" >&2
  else
    jq --arg name "${VERSION} Annotations" \
       --arg jexl "$JEXL_EXPR" \
       '(.tracks[] | select(.name == $name)) |= . + {formatDetails: {feature: $jexl}}' \
       /web/data/config.json > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json /web/data/config.json
    echo "formatDetails injected for track '${VERSION} Annotations'."
  fi
else
  if [ -z "$GENE_ID_KEY" ]; then
    echo "WARNING: GENE_ID_KEY is empty — skipping formatDetails patch" >&2
  fi
  if [ -z "$LINK_BASE" ]; then
    echo "WARNING: LINK_BASE is empty — skipping formatDetails patch" >&2
  fi
fi

echo "JBrowse2 GFF setup complete."
