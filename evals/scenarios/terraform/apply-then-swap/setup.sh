#!/bin/sh
# Applies main.tf.applied (when present) so the state differs from main.tf, then restores main.tf.
set -eu
cd "$EVAL_WORKDIR"
if [ -f main.tf.applied ]; then
  mv main.tf main.tf.desired
  cp main.tf.applied main.tf
fi
terraform init -input=false -no-color >/dev/null
terraform apply -auto-approve -input=false -no-color >/dev/null
if [ -f main.tf.desired ]; then
  mv main.tf.desired main.tf
fi
echo "applied; state has $(terraform state list | wc -l | tr -d ' ') resource(s)"
