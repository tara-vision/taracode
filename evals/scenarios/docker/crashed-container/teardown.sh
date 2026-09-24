#!/bin/sh
# Removes the crashed container so a re-recording starts clean.
set -eu
docker rm -f crashed-app >/dev/null 2>&1 || true
