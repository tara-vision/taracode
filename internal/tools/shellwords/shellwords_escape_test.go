package shellwords

import (
	"reflect"
	"testing"
)

// TestSplitOctalEscapeStopsAtANonOctalDigit: as in bash, $'\18' is the octal escape \1 followed by
// a literal 8, since 8 is no octal digit.
func TestSplitOctalEscapeStopsAtANonOctalDigit(t *testing.T) {
	res, err := Split(`echo $'\18'`)
	if err != nil || len(res.Segments) != 1 || !reflect.DeepEqual(res.Segments[0].Words, []string{"echo", "\x018"}) {
		t.Fatalf("%+v %v", res, err)
	}
}
