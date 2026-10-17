package scripts

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// signingSecrets are the five secrets that sign and notarize the macOS binaries of a release.
var signingSecrets = []string{"MACOS_SIGN_P12", "MACOS_SIGN_PASSWORD", "MACOS_NOTARY_ISSUER_ID", "MACOS_NOTARY_KEY_ID", "MACOS_NOTARY_KEY"}

// requireSigning runs scripts/require-signing.sh with the secrets of env and nothing else of the kind, and returns its
// exit status and what it printed.
func requireSigning(t *testing.T, env map[string]string) (int, string) {
	t.Helper()
	cmd := exec.Command("bash", "require-signing.sh")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for name, value := range env {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), out.String()
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, out.String()
}

func TestARunWithEverySigningSecretGoesOn(t *testing.T) {
	env := map[string]string{}
	for _, name := range signingSecrets {
		env[name] = "set"
	}
	if code, out := requireSigning(t, env); code != 0 || out != "" {
		t.Fatalf("exit %d: %s", code, out)
	}
}

func TestAReleaseStopsBeforeItPublishesUnsignedMacOSBinaries(t *testing.T) {
	for _, missing := range signingSecrets {
		t.Run(missing, func(t *testing.T) {
			env := map[string]string{}
			for _, name := range signingSecrets {
				env[name] = "set"
			}
			env[missing] = "" // what GitHub hands a step for a secret that is not set
			code, out := requireSigning(t, env)
			if code != 1 || !strings.Contains(out, missing+" is not set") || !strings.Contains(out, "signed") {
				t.Fatalf("exit %d: %s", code, out)
			}
		})
	}
}
