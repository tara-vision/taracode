package classify

import (
	"testing"

	"github.com/tara-vision/taracode/internal/policy"
)

// TestDateSetsTheClockOnlyWithSettingForms covers the date case of readProgramWrites, added when
// date became the replacement for the get_datetime tool: printing the time in any format or zone,
// or from a parsed input, is a read; -s/--set and a bare setting operand (GNU MMDDhhmm[[CC]YY],
// BSD [[[mm]dd]HH]MM[[cc]yy] and -f fmt new_date) set the system clock and are mutations, except
// under BSD's -j, which only parses.
func TestDateSetsTheClockOnlyWithSettingForms(t *testing.T) {
	reads := []string{
		"date", "date -u +%s", "date +%F", "date -R", "date -Iseconds", "date -d yesterday",
		"date --date='2 days ago' +%F", "date -r 0", "date -r /etc/hosts", "date -j 0101000020",
		"date -j -f %Y-%m-%d 2026-09-25 +%s", "date -v +1d", "TZ=Europe/Skopje date", "date --file=dates.txt",
		"TZ=UTC; date +%H $TZ",
	}
	for _, cmd := range reads {
		if res := Shell(cmd); res.Classification != policy.Read {
			t.Errorf("%q: %s (%s), want read", cmd, res.Classification, res.Reason)
		}
	}
	mutations := []string{
		"date -s '2020-01-01 00:00:00'", "date -s2020-01-01", "date --set='2020-01-01'", "date --set 2020-01-01",
		"date --se=2020-01-01", "date 0101000020", "date -u 0101000020", "date 0101000020.30",
		"date -f %Y-%m-%d 2026-09-25",
	}
	for _, cmd := range mutations {
		if res := Shell(cmd); res.Classification != policy.Mutate {
			t.Errorf("%q: %s, want mutate", cmd, res.Classification)
		}
	}
}
