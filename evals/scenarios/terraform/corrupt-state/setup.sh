#!/bin/sh
# The clean scenario with a corrupt state: main.tf is initialized and applied as it is, then the corrupt
# terraform.tfstate the task's workdir carries replaces the applied state, so that plan, show and state list record
# what terraform answers on a state it cannot read, which is what a model sees for real. The workdir's file is
# moved aside for the apply and moved back: the planted file and the workdir's are one file.
set -eu
cd "$EVAL_WORKDIR"
mv terraform.tfstate corrupt.tfstate.aside
terraform init -input=false -no-color >/dev/null
terraform apply -auto-approve -input=false -no-color >/dev/null
mv -f corrupt.tfstate.aside terraform.tfstate
echo "applied, then the state replaced by the corrupt file of $(wc -c < terraform.tfstate | tr -d ' ') bytes"
