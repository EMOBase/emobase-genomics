#!/bin/bash
set -e

VERSION="$1"

if [ -z "$VERSION" ]; then
  echo "Usage: $0 <version>" >&2
  exit 1
fi

DATA_DIR="/web/data"
CONFIG="$DATA_DIR/config.json"

if [ ! -f "$CONFIG" ]; then
  echo "No JBrowse2 config.json found at $CONFIG — nothing to clean up."
  exit 0
fi

echo "Removing JBrowse2 assembly, tracks, and text-search entries for version ${VERSION}..."
jq --arg version "$VERSION" '
  .assemblies |= map(select(.name != $version)) |
  .tracks |= (
    map(
      if ((.assemblyNames // []) | index($version)) != null
      then .assemblyNames |= map(select(. != $version))
      else .
      end
    )
    | map(select((.assemblyNames // []) | length > 0))
  ) |
  .aggregateTextSearchAdapters |= (
    (. // [])
    | map(
        if ((.assemblyNames // []) | index($version)) != null
        then .assemblyNames |= map(select(. != $version))
        else .
        end
      )
    | map(select((.assemblyNames // []) | length > 0))
  )
' "$CONFIG" > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json "$CONFIG"

# All assembly/track files for a version are written with a "<version>." filename
# prefix (see add_jbrowse_track.sh, setup_jbrowse2_fna.sh, setup_jbrowse2_gff.sh),
# so cleanup is a straightforward prefix match.
echo "Removing JBrowse2 data files for version ${VERSION}..."
find "$DATA_DIR" -maxdepth 1 -type f -name "${VERSION}.*" -delete

if [ -d "$DATA_DIR/trix" ]; then
  find "$DATA_DIR/trix" -maxdepth 1 -type f \
    \( -name "${VERSION}.*" -o -name "${VERSION}_meta.json" \) -delete
fi

echo "JBrowse2 cleanup complete for version ${VERSION}."
