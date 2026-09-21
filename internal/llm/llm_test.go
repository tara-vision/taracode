package llm

import "testing"

func TestParseThink(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Think
		ok   bool
	}{
		{"empty defaults to auto", "", ThinkAuto, true},
		{"explicit auto", "auto", ThinkAuto, true},
		{"off", "off", ThinkOff, true},
		{"false spells off", "false", ThinkOff, true},
		{"no spells off", "no", ThinkOff, true},
		{"on", "on", ThinkOn, true},
		{"true spells on", "true", ThinkOn, true},
		{"yes spells on", "yes", ThinkOn, true},
		{"low", "low", ThinkLow, true},
		{"medium", "medium", ThinkMedium, true},
		{"high", "high", ThinkHigh, true},
		{"unrecognized", "bogus", ThinkAuto, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseThink(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("ParseThink(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestModelDetailsHas(t *testing.T) {
	var nilDetails *ModelDetails
	if nilDetails.Has("tools") {
		t.Fatal("nil *ModelDetails should report no capabilities")
	}

	d := &ModelDetails{Capabilities: []string{"completion", "tools"}}
	if !d.Has("tools") {
		t.Fatal("expected \"tools\" capability to be reported present")
	}
	if d.Has("vision") {
		t.Fatal("expected \"vision\" capability to be reported absent")
	}
}
