#!/bin/bash
set -e

VERSION="$1"

if [ -z "$VERSION" ]; then
  echo "Usage: $0 <version>" >&2
  exit 1
fi

# Serialize access to the shared config.json: multiple JBrowse2 setup/track/
# delete scripts can run concurrently (see docker-compose worker replicas),
# and each does a non-atomic read-modify-write of that file.
exec 200>/web/data/.jbrowse-config.lock
flock -x 200

echo "Promoting JBrowse2 default view for assembly ${VERSION}..."

jq --arg assembly "$VERSION" '
  (.assemblies // [] | map(.name)) as $assemblyNames
  | .defaultSession.views |= (. // [])
  # Drop any defaultSession view whose assembly no longer exists (e.g. left
  # behind by a version delete that predates this cleanup, or a stale entry
  # from before this default-view promotion existed).
  | .defaultSession.views |= [ .[] | select(.init.assembly as $a | $assemblyNames | index($a)) ]
  # Move this assembly view to the front so JBrowse2 opens it first when
  # loaded with no explicit session, without discarding the other,
  # still-valid views.
  | (.defaultSession.views | map(.init.assembly == $assembly) | index(true)) as $idx
  | if $idx != null then
      .defaultSession.views |= ([.[$idx]] + .[0:$idx] + .[($idx + 1):])
    else . end
  | (first(.defaultSession.views[] | select(.init.assembly == $assembly) | .id) // null) as $viewId
  | if $viewId != null then
      .defaultSession.widgets.hierarchicalTrackSelector = {
        id: "hierarchicalTrackSelector",
        type: "HierarchicalTrackSelectorWidget",
        view: $viewId,
        filterText: ""
      }
      | .defaultSession.activeWidgets.hierarchicalTrackSelector = "hierarchicalTrackSelector"
    else . end
' /web/data/config.json > /tmp/_jbrowse_config.json && mv /tmp/_jbrowse_config.json /web/data/config.json

echo "JBrowse2 default view updated."
