package evals

import (
	"os"
	"testing"
)

func TestSetCallEnvRestoresAnUnsetKey(t *testing.T) {
	const key = "TARACODE_EVALS_TEST_ENV"
	t.Setenv(key, "placeholder") // the test's own cleanup restores the environment
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	restore := setCallEnv(map[string]string{key: "set-for-the-call"})
	if os.Getenv(key) != "set-for-the-call" {
		t.Fatalf("set: %q", os.Getenv(key))
	}
	restore()
	if v, ok := os.LookupEnv(key); ok {
		t.Fatalf("the key is unset again, not %q", v)
	}
}
