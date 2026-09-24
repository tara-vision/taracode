#!/bin/sh
# The clean setup, then app.conf is edited outside terraform so the directory drifts from its state.
set -eu
cd "$EVAL_WORKDIR"
terraform init -input=false -no-color >/dev/null
terraform apply -auto-approve -input=false -no-color >/dev/null
echo "applied; state has $(terraform state list | wc -l | tr -d ' ') resource(s)"
printf 'listen=9090\nchanged outside terraform\n' > app.conf
