package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".taracode", "permissions.json")
	p, ignored, err := LoadPermissions(path)
	if err != nil || ignored {
		t.Fatalf("missing file: %v %v", err, ignored)
	}
	if p.For("kubectl") != Ask {
		t.Fatal("default must be ask")
	}
	if err := p.Set("kubectl", Allow); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("*", Deny); err != nil {
		t.Fatal(err)
	}
	again, _, err := LoadPermissions(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.For("kubectl") != Allow || again.For("shell") != Deny {
		t.Fatalf("rules %v", again.Rules())
	}
	if err := again.Reset(); err != nil {
		t.Fatal(err)
	}
	if again.For("kubectl") != Ask || len(again.Rules()) != 0 {
		t.Fatalf("reset left %v", again.Rules())
	}
}

func TestPermissionsIgnoresAVersionOneFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "permissions.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"categories":{"read":"allow"},"tools":{"write_file":"deny"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, ignored, err := LoadPermissions(path)
	if err != nil || !ignored {
		t.Fatalf("%v %v", err, ignored)
	}
	if p.For("write_file") != Ask {
		t.Fatal("v1 rules must not apply")
	}
	if _, _, err := LoadPermissions(writeTemp(t, "{not json")); err == nil {
		t.Fatal("corrupt file must error")
	}
}

func TestAllowAllNeverAsks(t *testing.T) {
	if AllowAll().For("anything") != Allow {
		t.Fatal("AllowAll")
	}
	if _, ok := ParsePermission("ALLOW"); !ok {
		t.Fatal("ParsePermission is case-insensitive")
	}
	if _, ok := ParsePermission("maybe"); ok {
		t.Fatal("ParsePermission rejects unknown values")
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
