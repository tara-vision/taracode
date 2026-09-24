#!/bin/sh
# Initializes and applies main.tf as it is, so the state matches the configuration exactly.
set -eu
cd "$EVAL_WORKDIR"
terraform init -input=false -no-color >/dev/null
terraform apply -auto-approve -input=false -no-color >/dev/null
echo "applied; state has $(terraform state list | wc -l | tr -d ' ') resource(s)"
