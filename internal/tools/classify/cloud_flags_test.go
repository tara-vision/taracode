package classify

import "testing"

// TestAzureVerbIsReadFromTheCommandPathOnly: an az line is its command path (group, subgroup, verb)
// followed by flags. The verb used to be "the last word that is not a flag", and only eleven flags
// were known to take a value, so the value of any other flag became that last word: a repository,
// a pattern or an argument that starts with a read word turned a delete into a read.
func TestAzureVerbIsReadFromTheCommandPathOnly(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"az acr repository delete --name myreg --repository test", "delete"},
		{"az acr repository delete --name myreg --repository list", "delete"},
		{"az storage blob delete-batch --source c --pattern test-logs", "delete-batch"},
		{"az vm delete --ids /subscriptions/s/vm --no-wait validate", "delete"},
		{"az group delete --name show --yes", "delete"},
		{"az search service delete -n x -g y", "delete"},
		{"az config unset show_progress", "unset"},
		{"az config set list=1", "set"},
		{"az --only-show-errors vm delete -n x -g y", "az"},
		{"az --unknown show vm delete -n x -g y", "az"},
		{"az vm frobnicate -n x -g y", "frobnicate"},
	})
	checkReads(t, []string{
		"az", "az --version", "az --help", "az version", "az group list", "az account show",
		"az vm show -n x -g y --subscription sub-1", "az vm list --query [].name -o table",
		"az storage blob list -c x --account-name test", "az search service list -g x",
		"az acr repository show-tags --name myreg --repository delete",
		"az --subscription sub-1 vm list", "az deployment group list -g x", "az pipelines runs list",
	})
}

// TestCloudFlagsBeforeTheVerbCannotMoveIt: aws and gcloud take their global flags anywhere, also
// before the service or the verb. A flag the classifier does not know may take a value, and that
// value would then be read as the service or the verb: `--verbosity info` in front of a delete made
// it a read. Known value flags are skipped with their value; an unknown flag before the verb fails
// closed.
func TestCloudFlagsBeforeTheVerbCannotMoveIt(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"gcloud --verbosity info compute instances delete vm --quiet", "delete"},
		{"gcloud --impersonate-service-account list-sa@p.iam.gserviceaccount.com compute instances delete vm", "delete"},
		{"gcloud --unknown-flag describe compute instances delete vm", "gcloud"},
		{"gcloud compute instances delete vm --labels list", "delete"},
		{"gcloud firebase test android run --type robo", "run"},
		{"gcloud compute instances delete describe-x", "delete"},
		{"gcloud compute instances frobnicate vm --note list", "frobnicate"},
		{"aws --ca-bundle a --ca-bundle describe-instances ec2 terminate-instances --instance-ids i-1", "terminate-instances"},
		{"aws ec2 --ca-bundle describe-instances terminate-instances --instance-ids i-1", "terminate-instances"},
		{"aws --unknown-flag x describe-instances ec2 terminate-instances", "aws"},
		{"aws ec2 --unknown-flag describe-instances terminate-instances", "aws"},
	})
	checkReads(t, []string{
		"gcloud --verbosity info compute instances list", "gcloud --project p compute instances list",
		"gcloud compute instances list --filter name=delete", "gcloud --format=json projects describe p",
		"aws --region eu-west-1 ec2 describe-instances", "aws ec2 --region eu-west-1 describe-instances",
		"aws --no-cli-pager --debug s3 ls", "aws --color off --ca-bundle b.pem sts get-caller-identity",
		"aws --output=json ec2 describe-instances --filters Name=tag:delete,Values=x",
	})
}

// TestReadLookingVerbsThatAct: a few operations carry a read prefix and still do something.
func TestReadLookingVerbsThatAct(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"gcloud compute instances simulate-maintenance-event vm --zone z", "simulate-maintenance-event"},
		{"aws apigateway test-invoke-method --rest-api-id a --resource-id r --http-method POST", "test-invoke-method"},
		{"aws apigateway test-invoke-authorizer --rest-api-id a --authorizer-id b", "test-invoke-authorizer"},
	})
	checkMutations(t, []hardeningCase{
		{"aws elasticache test-failover --replication-group-id g --node-group-id 0001", "test-failover"},
		{"aws codecommit test-repository-triggers --repository-name r --triggers t", "test-repository-triggers"},
		{"aws cognito-idp get-user-attribute-verification-code --access-token t --attribute-name email",
			"get-user-attribute-verification-code"},
	})
	checkReads(t, []string{"aws iam simulate-principal-policy --policy-source-arn a --action-names s3:GetObject",
		"aws events test-event-pattern --event-pattern p --event e", "aws route53 test-dns-answer --hosted-zone-id z",
		"gcloud projects test-iam-permissions p --permissions x"})
}
