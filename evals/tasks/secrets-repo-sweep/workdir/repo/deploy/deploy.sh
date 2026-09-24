#!/bin/sh
# Deploys the latest build to the target environment.
set -eu
export API_TOKEN=tk_9f8e7d6c5b4a3210ffee
curl -sf -H "Authorization: Bearer $API_TOKEN" https://deploy.example.com/api/releases -d '{"service":"sample-service"}'
