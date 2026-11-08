#!/bin/sh
# linuxserver.io / arr-style entrypoint.
# Starts as root, makes /data writable by the requested PUID:PGID, then drops
# privileges to that user before exec'ing the app.
set -e

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"

mkdir -p /data
chown -R "${PUID}:${PGID}" /data

echo "Starting yomi-reader as ${PUID}:${PGID}"

exec su-exec "${PUID}:${PGID}" "$@"
