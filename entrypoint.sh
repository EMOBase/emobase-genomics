#!/bin/bash
set -e

# Ensure the temp directory for JBrowse2 setup exists on the uploads volume.
mkdir -p /app/public/uploads/.tmp

# Populate the JBrowse2 web app into the volume on first run.
# The volume is mounted at /web — if index.html is absent, the app hasn't been created yet.
if [ ! -f /web/index.html ]; then
  echo "JBrowse2 not found in /web, running jbrowse create..."
  jbrowse create /web
  echo "JBrowse2 created."
fi

exec "$@"
