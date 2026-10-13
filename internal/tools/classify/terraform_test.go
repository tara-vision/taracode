package classify

import "testing"

func TestTerraformDestroyFlagReadsTheLastWordAsTerraformDoes(t *testing.T) {
	cases := []struct {
		words []string
		want  bool
	}{
		{[]string{}, false},
		{[]string{"-destroy"}, true},
		{[]string{"--destroy"}, true},
		{[]string{"-destroy=true"}, true},
		{[]string{"-destroy=false"}, false},
		{[]string{"-destroy", "-destroy=false"}, false},
		{[]string{"-destroy=false", "-destroy"}, true},
		{[]string{"-destroy=maybe"}, false},
		{[]string{"-auto-approve", "-lock=false"}, false},
	}
	for _, c := range cases {
		if got := TerraformDestroyFlag(c.words); got != c.want {
			t.Errorf("TerraformDestroyFlag(%q) = %v, want %v", c.words, got, c.want)
		}
	}
}
