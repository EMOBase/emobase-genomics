#!/bin/bash
set -e

# Only the dedicated jbrowse worker needs to initialise the shared /web volume.
# Other containers skip this to avoid downloading JBrowse2 on every start.
if [ "${JBROWSE_ENABLED}" = "true" ] && [ ! -f /web/index.html ]; then
  echo "JBrowse2 not found in /web, running jbrowse create..."
  jbrowse create /web
  echo "JBrowse2 created."
fi

exec "$@"
