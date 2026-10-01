package redact

import "testing"

// TestContainsSecretFindsALabelledValue: for a pattern that keeps its label, a raw value is a secret.
func TestContainsSecretFindsALabelledValue(t *testing.T) {
	for in, want := range map[string]string{
		"password: hunter22":                     "credential",
		"clone https://admin:s3cret@example.com": "url-password",
	} {
		if kind, found := ContainsSecret(in); !found || kind != want {
			t.Errorf("ContainsSecret(%q) = %q, %v; want %q", in, kind, found, want)
		}
	}
}
