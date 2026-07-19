#!/bin/bash
set -e

GENOMIC_GFF_GZ="$1"
VERSION="$2"
GENE_ID_KEY="${3:-}"
LINK_BASE="${4:-}"
TRIM_PREFIX="${5:-0}"
TRIM_SUFFIX="${6:-0}"

if [ -z "$GENOMIC_GFF_GZ" ] || [ -z "$VERSION" ]; then
  echo "Usage: $0 <genomic_gff.gz> <version> [gene_id_key] [link_base] [trim_prefix_chars] [trim_suffix_chars]" >&2
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

# Run text-index before the jq patches below: text-index rewrites config.json
# and would overwrite anything we inject before it runs.
jbrowse text-index --out /web/data

echo "Selecting annotation track by default for version ${VERSION}..."

# Default the view initial location to the assembly first contig (full
# length), read from the .fai index written by setup_jbrowse2_fna.sh (that
# job always completes before this one is enqueued, so the file is present).
FAI_FILE="/web/data/${VERSION}.genomic.fna.fai"
LOC=""
if [ -f "$FAI_FILE" ]; then
  LOC=$(awk -F'\t' 'NR==1{print $1":1-"$2}' "$FAI_FILE")
fi

jq --arg assembly "$VERSION" --arg trackName "${VERSION} Annotations" --arg loc "$LOC" '
  (first(.tracks[] | select(.name == $trackName) | .trackId)) as $trackId |
  .defaultSession.views |= (. // []) |
  if (.defaultSession.views | any(.init.assembly == $assembly))
  then
    .defaultSession.views |= map(
      if .init.assembly == $assembly
      then .init.tracks = (
        (.init.tracks // []) as $existing
        | if ($existing | index($trackId)) then $existing else $existing + [$trackId] end
      )
      else .
      end
    )
  else
    .defaultSession.views += [{
      id: ("view-" + $assembly),
      type: "LinearGenomeView",
      init: (if $loc == "" then {assembly: $assembly, tracks: [$trackId]}
             else {assembly: $assembly, loc: $loc, tracks: [$trackId]} end)
    }]
  end
  # Open the "Available Tracks" panel (the JBrowse2 hierarchical track
  # selector) by default, pointed at the view for this assembly. Look up
  # the view id just created/updated rather than assuming a naming
  # convention, since hand-authored views may not follow "view-<assembly>".
  | (first(.defaultSession.views[] | select(.init.assembly == $assembly) | .id) // ("view-" + $assembly)) as $viewId
  | .defaultSession.widgets.hierarchicalTrackSelector = {
      id: "hierarchicalTrackSelector",
      type: "HierarchicalTrackSelectorWidget",
      view: $viewId,
      filterText: ""
    }
  | .defaultSession.activeWidgets.hierarchicalTrackSelector = "hierarchicalTrackSelector"
' /web/data/config.json > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json /web/data/config.json

echo "Annotation track added to default session."

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
    # Apply trim via subseq(str, start, end) which wraps String.slice().
    # -0 == 0 in JS so omit end entirely when suffix trim is 0.
    if [ "$TRIM_SUFFIX" -gt 0 ]; then
      TRIMMED="slice(${EXTRACT},${TRIM_PREFIX},-${TRIM_SUFFIX})"
    else
      TRIMMED="slice(${EXTRACT},${TRIM_PREFIX})"
    fi
    JEXL_EXPR="jexl:{emobase_link:${GUARD} ? '<a href=${LINK_BASE}'+${TRIMMED}+'>'+${TRIMMED}+'</a>' : ''}"
  else
    ID_EXPR="feature.${GENE_ID_KEY,,}"
    if [ "$TRIM_SUFFIX" -gt 0 ]; then
      TRIMMED="slice(${ID_EXPR},${TRIM_PREFIX},-${TRIM_SUFFIX})"
    else
      TRIMMED="slice(${ID_EXPR},${TRIM_PREFIX})"
    fi
    JEXL_EXPR="jexl:{emobase_link:${ID_EXPR} ? '<a href=${LINK_BASE}'+${TRIMMED}+'>'+${TRIMMED}+'</a>' : ''}"
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
