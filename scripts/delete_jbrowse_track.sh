#!/bin/bash
# Deletes one JBrowse2 track: its config entry, its place in the default
# session, and the data files `jbrowse add-track --load copy` copied into the
# web root (the data file and its index). `jbrowse remove-track` alone only
# removes the config entry, which left the copied files publicly served under
# /jbrowse2/ and the default session pointing at a track that no longer exists.
#
# Usage: delete_jbrowse_track.sh <track_id>
set -euo pipefail

TRACK_ID="${1:-}"
if [ -z "$TRACK_ID" ]; then
  echo "Usage: $0 <track_id>" >&2
  exit 1
fi

DATA_DIR="/web/data"
CONFIG="$DATA_DIR/config.json"

# Serialize access to the shared config.json, like every other script that
# writes it (see add_jbrowse_track.sh).
exec 200>"$DATA_DIR/.jbrowse-config.lock"
flock -x 200

# The files this track uses, read before its entry is removed.
TRACK_URIS=$(jq -r --arg id "$TRACK_ID" '.tracks[]? | select(.trackId == $id) | .. | .uri? // empty' "$CONFIG")

jbrowse remove-track "$TRACK_ID" --out "$DATA_DIR"

jq --arg id "$TRACK_ID" '
  if .defaultSession.views then
    .defaultSession.views |= map(
      if .init.tracks then .init.tracks |= map(select(. != $id)) else . end
    )
  else . end
' "$CONFIG" > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json "$CONFIG"

# A file another track still uses stays: before stored file names carried the
# upload ID, two same-named tracks of one assembly shared one copied file.
STILL_USED=$(jq -r '.. | .uri? // empty' "$CONFIG")

while IFS= read -r uri; do
  [ -n "$uri" ] || continue
  # Only a plain file name inside the data folder: never a path, ".." or a URL.
  case "$uri" in
    */* | . | ..)
      echo "Not removing '$uri': not a file name in $DATA_DIR"
      continue
      ;;
  esac
  if grep -Fxq -- "$uri" <<<"$STILL_USED"; then
    echo "Keeping $uri: another track still uses it"
    continue
  fi
  rm -f -- "$DATA_DIR/$uri"
  echo "Removed $DATA_DIR/$uri"
done <<<"$TRACK_URIS"

echo "JBrowse2 track ${TRACK_ID} deleted."
