package models

import "testing"

func TestHostRAMGBIsPositive(t *testing.T) {
	gb, err := HostRAMGB()
	if err != nil || gb <= 0 {
		t.Fatalf("ram=%d err=%v", gb, err)
	}
}

func TestParseMeminfo(t *testing.T) {
	if gb := parseMeminfo("MemTotal:       49364052 kB\nMemFree: 1 kB\n"); gb != 47 {
		t.Fatalf("parseMeminfo = %d, want 47", gb)
	}
	if gb := parseMeminfo("garbage"); gb != 0 {
		t.Fatalf("parseMeminfo(garbage) = %d", gb)
	}
}

func TestParseMeminfoMissingValue(t *testing.T) {
	if gb := parseMeminfo("MemTotal:\n"); gb != 0 {
		t.Fatalf("parseMeminfo(no value) = %d, want 0", gb)
	}
}

func TestParseMeminfoNonNumericValue(t *testing.T) {
	if gb := parseMeminfo("MemTotal:       notanumber kB\n"); gb != 0 {
		t.Fatalf("parseMeminfo(non-numeric) = %d, want 0", gb)
	}
}
