package docscheck

import (
	"cmp"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture's sponsors by position, so that a case names the entry it breaks.
const (
	titleCo = iota
	labCo
	teamCo
	hoursCo
	boardCo
)

// enDash is built from its code point, so that no fancy character appears in this source.
var enDash = string(rune(0x2013))

// fixtureMonth is the month the sponsors fixtures are checked in, as the current month: the first month of every
// sponsor of cleanDoc.
const fixtureMonth = "2026-11"

// gone are marks that point at nothing on the boards of testFacts, one of each kind.
func gone() marks {
	return marks{Models: []string{"mistral"}, Machines: []string{"NVIDIA RTX 4090 (24 GB)"}, Engines: []string{"vllm"}}
}

// testFacts are the boards the fixtures are checked against: two machines, three models and two engines.
func testFacts() boardFacts {
	return boardFacts{
		Machines: map[string]bool{"Apple M4 Max (48 GB)": true, "NVIDIA RTX 5090 (32 GB)": true},
		Models:   []string{"gemma4:12b", "glm-4.7-flash", "qwen3.8:27b"},
		Engines:  []string{"Ollama 0.35.0", "taraengine 0.3.1"},
	}
}

// cleanDoc is a sponsors file that breaks no rule: one sponsor per tier, a Lab sponsor and a Supporter whose marks
// point at testFacts, and two paid runs (a named buyer and an unnamed one). Every call returns a fresh copy for a case
// to break.
func cleanDoc() sponsorsDoc {
	return sponsorsDoc{
		Schema: 1,
		Sponsors: []sponsor{
			{ID: "title-co", Name: "Title Co", Tier: "title", URL: "https://title.example", Since: "2026-11"},
			{ID: "lab-co", Name: "Lab Co", Tier: "lab-sponsor", URL: "https://lab.example", Since: "2026-11",
				Marks: marks{Models: []string{"gemma"}, Machines: []string{"NVIDIA RTX 5090 (32 GB)"},
					Engines: []string{"taraengine"}}},
			{ID: "team-co", Name: "Team Co", Tier: "team", URL: "https://team.example", Since: "2026-11",
				Until: strPtr("2027-04")},
			{ID: "hours-co", Name: "Hours Co", Tier: "lab-hours", URL: "https://github.com/hours-co", Since: "2026-11"},
			{ID: "board-co", Name: "Board Co", Tier: "supporter", URL: "https://board.example", Since: "2026-11",
				Marks: marks{Models: []string{"glm"}}},
		},
		Runs: []run{
			{ID: "run-one", Model: "qwen3.8:27b", Month: "2026-11", By: strPtr("Board Co"),
				URL: strPtr("https://board.example")},
			{ID: "run-two", Model: "gemma4:12b", Month: "2026-12"},
		},
	}
}

// another is a valid sponsor of the tier that starts in 2026-11, without marks.
func another(id, tier string) sponsor {
	return sponsor{ID: id, Name: id, Tier: tier, URL: "https://" + id + ".example", Since: "2026-11"}
}

// starting is a copy of s that begins in month.
func starting(s sponsor, month string) sponsor {
	s.Since = month
	return s
}

// ending is a copy of s whose last month is month.
func ending(s sponsor, month string) sponsor {
	s.Until = &month
	return s
}

// logosOf is the logo files of a document: one for every sponsor but the one named without.
func logosOf(d sponsorsDoc, without string) map[string]bool {
	logos := map[string]bool{}
	for _, s := range d.Sponsors {
		logos[s.ID] = s.ID != without
	}
	return logos
}

// TestCheckSponsorsCatchesEachRule: a clean file gives no problems; every case breaks one rule and gets exactly one
// message, carrying the rule's key. The cases without a key are the edges that must stay clean.
func TestCheckSponsorsCatchesEachRule(t *testing.T) {
	cases := []struct {
		name   string
		want   string // the key the one message carries; empty for a file that stays clean
		noLogo string // the id of a sponsor whose logo file is missing
		today  string // the current month the file is checked in; empty for fixtureMonth
		edit   func(d *sponsorsDoc)
	}{
		{name: "a clean file"},
		{name: "a schema the site does not read", want: "schema", edit: func(d *sponsorsDoc) {
			d.Schema = 2
		}},
		{name: "a schema of 0", want: "schema", edit: func(d *sponsorsDoc) {
			d.Schema = 0
		}},
		{name: "a repeated sponsor id", want: "duplicate id", edit: func(d *sponsorsDoc) {
			d.Sponsors = append(d.Sponsors, d.Sponsors[hoursCo])
		}},
		{name: "a sponsor id that is not a slug", want: "not a slug", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].ID = "Hours Co"
		}},
		{name: "an unknown tier", want: "unknown tier", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].Tier = "gold"
		}},
		{name: "a sponsor without a name", want: "empty name", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].Name = ""
		}},
		{name: "a sponsor name of spaces", want: "empty name", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].Name = "  "
		}},
		{name: "a sponsor link that is not https", want: "not https", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].URL = "http://hours.example"
		}},
		{name: "a first month that is not a month", want: "not a month", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].Since = "2026-13"
		}},
		{name: "a last month that is not a month", want: "not a month", edit: func(d *sponsorsDoc) {
			d.Sponsors[teamCo].Until = strPtr("2027-4")
		}},
		{name: "a last month before the first", want: "until before since", edit: func(d *sponsorsDoc) {
			d.Sponsors[teamCo].Until = strPtr("2026-10")
		}},
		{name: "two title sponsors", want: "two title sponsors in 2026-11", edit: func(d *sponsorsDoc) {
			d.Sponsors = append(d.Sponsors, another("title-two", "title"))
		}},
		{name: "a title sponsor starting in the month the last one ends", want: "two title sponsors in 2026-12",
			edit: func(d *sponsorsDoc) {
				d.Sponsors[titleCo] = ending(d.Sponsors[titleCo], "2026-12")
				d.Sponsors = append(d.Sponsors, starting(another("title-two", "title"), "2026-12"))
			}},
		{name: "a title sponsor handing over to the next one the month after", edit: func(d *sponsorsDoc) {
			d.Sponsors[titleCo] = ending(d.Sponsors[titleCo], "2026-12")
			d.Sponsors = append(d.Sponsors, starting(another("title-two", "title"), "2027-01"))
		}},
		{name: "four lab sponsors", want: "four lab sponsors in 2026-11", edit: func(d *sponsorsDoc) {
			d.Sponsors = append(d.Sponsors, another("lab-two", "lab-sponsor"), another("lab-three", "lab-sponsor"),
				another("lab-four", "lab-sponsor"))
		}},
		{name: "three lab sponsors", edit: func(d *sponsorsDoc) {
			d.Sponsors = append(d.Sponsors, another("lab-two", "lab-sponsor"), another("lab-three", "lab-sponsor"))
		}},
		{name: "a fourth lab sponsor once the first has ended", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo] = ending(d.Sponsors[labCo], "2026-12")
			d.Sponsors = append(d.Sponsors, another("lab-two", "lab-sponsor"), another("lab-three", "lab-sponsor"),
				starting(another("lab-four", "lab-sponsor"), "2027-01"))
		}},
		{name: "a title sponsor with marks", want: "title with marks", edit: func(d *sponsorsDoc) {
			d.Sponsors[titleCo].Marks = marks{Models: []string{"gemma"}}
		}},
		{name: "a supporter without marks", want: "supporter without marks", edit: func(d *sponsorsDoc) {
			d.Sponsors[boardCo].Marks = marks{}
		}},
		{name: "a machine that is on no board", want: "machine not on the boards", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks.Machines = []string{"NVIDIA RTX 4090 (24 GB)"}
		}},
		{name: "a machine label in another case", want: "machine not on the boards", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks.Machines = []string{"nvidia rtx 5090 (32 gb)"}
		}},
		{name: "a machine label without its memory", want: "machine not on the boards", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks.Machines = []string{"NVIDIA RTX 5090"}
		}},
		{name: "a model prefix that matches no row", want: "model prefix matches no row", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks.Models = []string{"mistral"}
		}},
		{name: "an empty model prefix", want: "model prefix matches no row", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks.Models = []string{""}
		}},
		{name: "an engine prefix that matches no row", want: "engine prefix matches no row", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks.Engines = []string{"vllm"}
		}},
		{name: "a team sponsor without a logo", want: "no logo for team", noLogo: "team-co"},
		{name: "a fancy dash in a sponsor name", want: "fancy character", edit: func(d *sponsorsDoc) {
			d.Sponsors[titleCo].Name = "Title " + enDash + " Co"
		}},
		{name: "a fancy dash in a run", want: "fancy character", edit: func(d *sponsorsDoc) {
			d.Runs[0].Model = "qwen3.8" + enDash + "27b"
		}},
		{name: "a repeated run id", want: "duplicate run id", edit: func(d *sponsorsDoc) {
			d.Runs = append(d.Runs, d.Runs[1])
		}},
		{name: "a run id that is not a slug", want: "not a slug", edit: func(d *sponsorsDoc) {
			d.Runs[1].ID = "Run Two"
		}},
		{name: "a run month that is not a month", want: "run month", edit: func(d *sponsorsDoc) {
			d.Runs[1].Month = "2026-13"
		}},
		{name: "a named buyer without a link", want: "not https", edit: func(d *sponsorsDoc) {
			d.Runs[0].URL = nil
		}},
		{name: "a buyer link that is not https", want: "not https", edit: func(d *sponsorsDoc) {
			d.Runs[0].URL = strPtr("http://board.example")
		}},
		{name: "a link without a buyer that is not https", want: "not https", edit: func(d *sponsorsDoc) {
			d.Runs[1].URL = strPtr("http://nobody.example")
		}},
		{name: "a run without a model", want: "empty run model", edit: func(d *sponsorsDoc) {
			d.Runs[1].Model = ""
		}},
		{name: "a run model of spaces", want: "empty run model", edit: func(d *sponsorsDoc) {
			d.Runs[1].Model = "  "
		}},
		{name: "a buyer named by an empty string", want: "empty buyer", edit: func(d *sponsorsDoc) {
			d.Runs[0].By = strPtr("")
		}},
		{name: "a buyer named by spaces", want: "empty buyer", edit: func(d *sponsorsDoc) {
			d.Runs[0].By = strPtr("  ")
		}},
		{name: "a model bought again in a later month", edit: func(d *sponsorsDoc) {
			d.Runs = append(d.Runs, run{ID: "run-three", Model: "qwen3.8:27b", Month: "2027-01"})
		}},
		{name: "a model bought before it is on any board", edit: func(d *sponsorsDoc) {
			d.Runs = append(d.Runs, run{ID: "run-new", Model: "not-on-the-boards:7b", Month: "2027-01"})
		}},
		{name: "prefixes ignore case", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks = marks{Models: []string{"Qwen"}, Engines: []string{"ollama"}}
		}},

		// A sponsorship that runs in the current month or later has its marks held to the boards; one that ended
		// before keeps the marks it had, so that a board refresh cannot fail an old record.
		{name: "a machine gone from the boards in the last month", want: "machine not on the boards",
			edit: func(d *sponsorsDoc) {
				d.Sponsors[labCo] = ending(d.Sponsors[labCo], fixtureMonth)
				d.Sponsors[labCo].Marks.Machines = gone().Machines
			}},
		{name: "a model gone from the boards in the last month", want: "model prefix matches no row",
			edit: func(d *sponsorsDoc) {
				d.Sponsors[labCo] = ending(d.Sponsors[labCo], fixtureMonth)
				d.Sponsors[labCo].Marks.Models = gone().Models
			}},
		{name: "an engine gone from the boards in the last month", want: "engine prefix matches no row",
			edit: func(d *sponsorsDoc) {
				d.Sponsors[labCo] = ending(d.Sponsors[labCo], fixtureMonth)
				d.Sponsors[labCo].Marks.Engines = gone().Engines
			}},
		{name: "marks gone from the boards after the sponsorship ended", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo] = ending(starting(d.Sponsors[labCo], "2026-01"), "2026-10")
			d.Sponsors[labCo].Marks = gone()
		}},
		{name: "marks gone from the boards, checked the month after the last", today: "2026-12",
			edit: func(d *sponsorsDoc) {
				d.Sponsors[labCo] = ending(d.Sponsors[labCo], fixtureMonth)
				d.Sponsors[labCo].Marks = gone()
			}},
		{name: "an ended supporter without marks", want: "supporter without marks", edit: func(d *sponsorsDoc) {
			d.Sponsors[boardCo] = ending(starting(d.Sponsors[boardCo], "2026-01"), "2026-10")
			d.Sponsors[boardCo].Marks = marks{}
		}},
		{name: "an ended title sponsor with marks", want: "title with marks", edit: func(d *sponsorsDoc) {
			d.Sponsors[titleCo] = ending(starting(d.Sponsors[titleCo], "2026-01"), "2026-10")
			d.Sponsors[titleCo].Marks = marks{Models: []string{"gemma"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := cleanDoc()
			if c.edit != nil {
				c.edit(&doc)
			}
			got := checkSponsors(doc, testFacts(), logosOf(doc, c.noLogo), cmp.Or(c.today, fixtureMonth))
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("want no problem, got %q", got)
			case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
				t.Errorf("want exactly one problem containing %q, got %q", c.want, got)
			}
		})
	}
}

// TestSchemaOfAFileIsChecked: a file that declares schema 2 or 0 is refused by the rules, and one that declares none by
// the decoding, since the site takes either as invalid and then shows no mark, credit or Title line at all.
func TestSchemaOfAFileIsChecked(t *testing.T) {
	for _, c := range []struct{ name, data, want string }{
		{"schema 1", `{"schema": 1, "sponsors": [], "runs": []}`, ""},
		{"schema 2", `{"schema": 2, "sponsors": [], "runs": []}`, "schema is 2"},
		{"schema 0", `{"schema": 0, "sponsors": [], "runs": []}`, "schema is 0"},
		{"no schema key", `{"sponsors": [], "runs": []}`, `missing key "schema"`},
	} {
		var doc sponsorsDoc
		got := problemsOf(c.data, &doc, func() []string { return checkSponsors(doc, testFacts(), nil, fixtureMonth) })
		switch {
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
			t.Errorf("%s: problems = %q, want exactly one containing %q", c.name, got, c.want)
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: problems = %q, want none", c.name, got)
		}
	}
}

// TestCheckSponsorsAsksForALogoOnlyWhereTheTierShowsOne: a Title, Lab sponsor or Team sponsor needs a logo file; a Lab
// hours sponsor or a Supporter is credited without one.
func TestCheckSponsorsAsksForALogoOnlyWhereTheTierShowsOne(t *testing.T) {
	for _, c := range []struct {
		tier  string
		wants bool
	}{{"title", true}, {"lab-sponsor", true}, {"team", true}, {"lab-hours", false}, {"supporter", false}} {
		s := another("no-logo-co", c.tier)
		if c.tier == "supporter" {
			s.Marks = marks{Models: []string{"gemma"}}
		}
		got := checkSponsors(sponsorsDoc{Schema: 1, Sponsors: []sponsor{s}}, testFacts(), map[string]bool{}, fixtureMonth)
		switch {
		case c.wants && (len(got) != 1 || !strings.Contains(got[0], "no logo for "+c.tier)):
			t.Errorf("a %s sponsor without a logo gave %q, want one problem about the logo", c.tier, got)
		case !c.wants && len(got) != 0:
			t.Errorf("a %s sponsor without a logo gave %q, want none", c.tier, got)
		}
	}
}

// TestEndedMeansALastMonthBeforeThisOne: a sponsorship has ended once its last month is before the current one, across
// a year too. One that runs on, one in its last month and one whose last month is not a month have not: their marks
// stay held to the boards.
func TestEndedMeansALastMonthBeforeThisOne(t *testing.T) {
	for _, c := range []struct {
		until *string
		month string
		want  bool
	}{
		{nil, "2026-11", false},
		{strPtr("2026-12"), "2026-11", false},
		{strPtr("2026-11"), "2026-11", false},
		{strPtr("2026-10"), "2026-11", true},
		{strPtr("2026-12"), "2027-01", true},
		{strPtr("2026-1"), "2026-11", false},
	} {
		if got := ended(sponsor{Since: "2026-01", Until: c.until}, c.month); got != c.want {
			t.Errorf("ended(until %q, in %s) = %v, want %v", orEmpty(c.until), c.month, got, c.want)
		}
	}
}

// TestIsSlugWantsLowercaseLettersDigitsAndHyphens: an id names a logo file, so nothing that could climb out of the
// folder or break a URL passes.
func TestIsSlugWantsLowercaseLettersDigitsAndHyphens(t *testing.T) {
	for _, s := range []string{"a", "example-co", "co2", "9"} {
		if !isSlug(s) {
			t.Errorf("isSlug(%q) = false, want true", s)
		}
	}
	accented := "caf" + string(rune(0xe9))
	for _, s := range []string{"", "Example", "ex ample", "ex_ample", "ex.ample", "a/b", "../x", accented, "a\n"} {
		if isSlug(s) {
			t.Errorf("isSlug(%q) = true, want false", s)
		}
	}
}

// TestIsHTTPSWantsAnHTTPSLinkWithAHost: a link starts with https:// and names a host.
func TestIsHTTPSWantsAnHTTPSLinkWithAHost(t *testing.T) {
	for _, u := range []string{"https://example.com", "https://github.com/sponsors/example", "https://example.com/a?b=c"} {
		if !isHTTPS(u) {
			t.Errorf("isHTTPS(%q) = false, want true", u)
		}
	}
	for _, u := range []string{"", "example.com", "http://example.com", "javascript:alert(1)", "//example.com",
		"ftp://example.com", "https://", "https:///path", "https:// example.com"} {
		if isHTTPS(u) {
			t.Errorf("isHTTPS(%q) = true, want false", u)
		}
	}
}

// The committed list and the folder of logos beside it, which does not exist until the first logo.
const (
	sponsorsPath = docsDir + "sponsors.json"
	logosGlob    = docsDir + "sponsors/*.svg"
)

// TestSponsorsFileFollowsTheRules: the committed sponsors.json, with the logos that sit beside it and the committed
// boards, passes every rule in the current month, so a pull request that breaks one fails in CI before the site reads
// the file.
func TestSponsorsFileFollowsTheRules(t *testing.T) {
	var doc sponsorsDoc
	readJSON(t, sponsorsPath, &doc)
	files, err := filepath.Glob(logosGlob)
	if err != nil {
		t.Fatal(err)
	}
	logos := map[string]bool{}
	for _, file := range files {
		logos[strings.TrimSuffix(filepath.Base(file), ".svg")] = true
	}
	for _, problem := range checkSponsors(doc, loadBoardFacts(t), logos, currentMonth()) {
		t.Error(problem)
	}
}
