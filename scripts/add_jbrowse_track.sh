#!/bin/bash
set -e

TRACK_GZ="$1"
TRACK_NAME="$2"
ASSEMBLY_NAME="$3"
TRACK_ID="$4"
CATEGORY="${5:-}"
SELECT_IN_DEFAULT_SESSION="${6:-false}"

if [ -z "$TRACK_GZ" ] || [ -z "$TRACK_NAME" ] || [ -z "$ASSEMBLY_NAME" ] || [ -z "$TRACK_ID" ]; then
  echo "Usage: $0 <track.gz> <track_name> <assembly_name> <track_id> [category] [select_in_default_session]" >&2
  exit 1
fi

DATA_DIR="/web/data"

TMPDIR=$(mktemp -d -p /jbrowse2-tmp)
trap "rm -rf $TMPDIR" EXIT

BASENAME=$(basename "$TRACK_GZ")
BASENAME="${BASENAME%.gzip}"
BASENAME="${BASENAME%.gz}"

# Prefix the filename with the assembly/version name so that files from
# different versions with the same original filename (e.g. tcas1.read_coverage.bw)
# are stored as distinct files in /web/data/ and don't overwrite each other.
case "$BASENAME" in
  *.gff|*.gff3)
    echo "Decompressing track file..."
    gunzip -c "$TRACK_GZ" > "$TMPDIR/$BASENAME"
    TRACK_FILE="$TMPDIR/$ASSEMBLY_NAME.${BASENAME%.*}.sorted.gff.gz"
    echo "Sorting and compressing GFF..."
    jbrowse sort-gff "$TMPDIR/$BASENAME" | bgzip > "$TRACK_FILE"
    tabix "$TRACK_FILE"
    ;;
  *)
    TRACK_FILE="$TMPDIR/$ASSEMBLY_NAME.$BASENAME"
    echo "Decompressing track file..."
    gunzip -c "$TRACK_GZ" > "$TRACK_FILE"
    ;;
esac

# Serialize access to the shared config.json from here on: multiple JBrowse2
# setup/track/delete scripts can run concurrently (see docker-compose worker
# replicas), and each does a non-atomic read-modify-write of that file. Held
# through the default-session patch below too, so both mutations for this
# track land as one atomic unit relative to other scripts.
exec 200>"$DATA_DIR/.jbrowse-config.lock"
flock -x 200

echo "Adding JBrowse2 track..."
CATEGORY_ARG=()
if [ -n "$CATEGORY" ]; then
  CATEGORY_ARG=(--category "$CATEGORY")
fi
jbrowse add-track "$TRACK_FILE" \
  --name "$TRACK_NAME" \
  --assemblyNames "$ASSEMBLY_NAME" \
  --trackId "$TRACK_ID" \
  --load copy \
  --out "$DATA_DIR" \
  --force \
  "${CATEGORY_ARG[@]}"

echo "JBrowse2 track added successfully."

if [ "$SELECT_IN_DEFAULT_SESSION" = "true" ]; then
  echo "Selecting track '${TRACK_ID}' by default for assembly '${ASSEMBLY_NAME}'..."

  # Default the view's initial location to the assembly's first contig
  # (full length), read from the .fai index written by setup_jbrowse2_fna.sh.
  FAI_FILE="$DATA_DIR/${ASSEMBLY_NAME}.genomic.fna.fai"
  LOC=""
  if [ -f "$FAI_FILE" ]; then
    LOC=$(awk -F'\t' 'NR==1{print $1":1-"$2}' "$FAI_FILE")
  fi

  CONFIG="$DATA_DIR/config.json"
  jq --arg assembly "$ASSEMBLY_NAME" --arg trackId "$TRACK_ID" --arg loc "$LOC" '
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
  ' "$CONFIG" > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json "$CONFIG"

  echo "Default track selection updated."
fi
