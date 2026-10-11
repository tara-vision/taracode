package models

import "testing"

func TestVersionsThatDoNotParse(t *testing.T) {
	if versionBefore("0.5.0", "not.a.version") {
		t.Fatal("an unparsable requirement is never newer")
	}
	if _, ok := parseVersion("1.2.3.4"); ok {
		t.Fatal("four components")
	}
}
