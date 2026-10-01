package classify

import "testing"

// TestBareAWSCommandsFailClosed: an aws line with one word after "aws" used to be a read whatever the
// word was. `aws configure` writes the credentials file from its standard input (a pipe or a
// redirect inside a shell line gives it one), and `aws login` and `aws logout` change the local
// credentials, so each of them ran in investigate mode without a prompt. Only `aws help` and a line
// with no word at all (the usage text, --version) are reads; any other single word has no read-only
// verb and fails closed.
func TestBareAWSCommandsFailClosed(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"aws configure", "configure"},
		{"aws configure --profile prod", "configure"},
		{"printf 'KEY\\nSECRET\\n\\n\\n' | aws configure", "configure"},
		{"cat creds.txt | aws configure", "configure"},
		{"aws configure < creds.txt", "configure"},
		{"AWS_PROFILE=dev aws configure", "configure"},
		{"aws login", "login"},
		{"aws logout", "logout"},
		{"aws sso", "sso"},
		{"aws --profile dev s3", "s3"},
	})
	checkReads(t, []string{"aws help", "aws --version", "aws", "aws --profile dev", "aws s3 help",
		"aws configure list", "aws configure get region", "aws sts get-caller-identity"})
}

// TestAHashInsideAWordDoesNotHideTheNextCommand: "#" starts a comment only at the start of a word.
// Were it to start one inside a word too, everything after `a#b` would be dropped before it is
// classified, and the command after the separator would run as part of a read.
func TestAHashInsideAWordDoesNotHideTheNextCommand(t *testing.T) {
	checkMutations(t, []hardeningCase{
		{"ls a#b; rm -rf build", "rm"},
		{"echo v1.2#rc; kubectl delete ns shop", "delete"},
		{"cat notes#1 && terraform destroy", "destroy"},
	})
	checkReads(t, []string{"ls a#b", "echo done # rm -rf build"})
}
