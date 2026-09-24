#!/bin/sh
# The api service's whole "application": it always fails to start, imitating a service that is
# missing a required setting. The reason lives here, inside the image, not in compose.yaml, so a
# model must inspect the running container (ps, logs) rather than read the compose file to learn it.
echo "api: DATABASE_URL is not set, refusing to start" >&2
exit 1
