package docscheck

import (
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// sponsorsDoc is docs/evals/sponsors.json: the credited sponsors and the paid runs.
type sponsorsDoc struct {
	Schema   int       `json:"schema"`
	Sponsors []sponsor `json:"sponsors"`
	Runs     []run     `json:"runs"`
}

// sponsor is one credited sponsor. Until is nil while the sponsorship runs.
type sponsor struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Tier  string  `json:"tier"`
	URL   string  `json:"url"`
	Since string  `json:"since"`
	Until *string `json:"until"`
	Marks marks   `json:"marks"`
}

// marks tie a sponsor to what the boards show: model-name prefixes, exact board hardware labels and engine-name
// prefixes.
type marks struct {
	Models   []string `json:"models"`
	Machines []string `json:"machines"`
	Engines  []string `json:"engines"`
}

// run is a paid run of a named model. By and URL name the buyer; both are nil for a buyer who stays unnamed.
type run struct {
	ID    string  `json:"id"`
	Model string  `json:"model"`
	Month string  `json:"month"`
	By    *string `json:"by"`
	URL   *string `json:"url"`
}

var (
	// knownTiers are the design's tiers; supporter is for a company on the boards only.
	knownTiers = []string{"title", "lab-sponsor", "team", "lab-hours", "supporter"}
	// logoTiers are the tiers shown with a logo, so with a file docs/evals/sponsors/<id>.svg.
	logoTiers = []string{"title", "lab-sponsor", "team"}
	// tierCaps are the tiers that take a limited number of current sponsors: one Title sponsor and three Lab
	// sponsors. noun is how a message names them.
	tierCaps = []struct {
		tier, noun string
		limit      int
	}{
		{"title", "title sponsors", 1},
		{"lab-sponsor", "lab sponsors", 3},
	}
)

// slugPattern is a lowercase slug, the form of an id.
var slugPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// countWords spell the small counts a message gives.
var countWords = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}

// label names a sponsor in a message.
func (s sponsor) label() string { return fmt.Sprintf("sponsor %q", s.ID) }

// label names a run in a message.
func (r run) label() string { return fmt.Sprintf("run %q", r.ID) }

// empty reports whether no mark is set.
func (m marks) empty() bool { return len(m.Models)+len(m.Machines)+len(m.Engines) == 0 }

// isSlug reports whether s is a lowercase slug: letters a to z, digits and hyphens.
func isSlug(s string) bool { return slugPattern.MatchString(s) }

// isHTTPS reports whether u is a link that starts with https:// and names a host.
func isHTTPS(u string) bool {
	if !strings.HasPrefix(u, "https://") {
		return false
	}
	parsed, err := url.Parse(u)
	return err == nil && parsed.Host != ""
}

// needsLogo reports whether a sponsor of the tier is shown with a logo, so its file must exist.
func needsLogo(tier string) bool { return slices.Contains(logoTiers, tier) }

// anyStartsWith reports whether one of names starts with prefix, ignoring case. An empty prefix names nothing, so a
// stray "" in a list of marks cannot mark every row.
func anyStartsWith(names []string, prefix string) bool {
	if prefix == "" {
		return false
	}
	lower := strings.ToLower(prefix)
	return slices.ContainsFunc(names, func(name string) bool { return strings.HasPrefix(strings.ToLower(name), lower) })
}

// validTerm reports whether both months of a sponsor are months, so that covers can compare them.
func validTerm(s sponsor) bool { return isMonth(s.Since) && (s.Until == nil || isMonth(*s.Until)) }

// countWord spells a small count, so that a message reads "two title sponsors".
func countWord(n int) string {
	if n < len(countWords) {
		return countWords[n]
	}
	return strconv.Itoa(n)
}

// checkSponsors applies the rules of the design to a sponsors file and returns one message per problem. f holds what
// the boards show, and logos the ids that have a file docs/evals/sponsors/<id>.svg. Every rule is a function of its
// own.
func checkSponsors(doc sponsorsDoc, f boardFacts, logos map[string]bool) []string {
	return slices.Concat(
		checkSponsorIDs(doc.Sponsors),
		checkSponsorTiers(doc.Sponsors),
		checkSponsorURLs(doc.Sponsors),
		checkSponsorMonths(doc.Sponsors),
		checkTierCaps(doc.Sponsors),
		checkMarks(doc.Sponsors, f),
		checkLogoPresence(doc.Sponsors, logos),
		checkPaidRuns(doc.Runs),
		checkSponsorsText(doc),
	)
}

// checkSponsorIDs: an id is a slug, since it names the logo file, and belongs to one sponsor.
func checkSponsorIDs(sponsors []sponsor) []string {
	var p findings
	seen := map[string]bool{}
	for _, s := range sponsors {
		if !isSlug(s.ID) {
			p.addf("%s: id is not a slug (lowercase letters, digits and hyphens)", s.label())
		}
		if seen[s.ID] {
			p.addf("duplicate id %q", s.ID)
		}
		seen[s.ID] = true
	}
	return p
}

// checkSponsorTiers: a sponsor holds one of the design's tiers.
func checkSponsorTiers(sponsors []sponsor) []string {
	var p findings
	for _, s := range sponsors {
		if !slices.Contains(knownTiers, s.Tier) {
			p.addf("%s: unknown tier %q", s.label(), s.Tier)
		}
	}
	return p
}

// checkSponsorURLs: every sponsor links to an https address, whatever the tier.
func checkSponsorURLs(sponsors []sponsor) []string {
	var p findings
	for _, s := range sponsors {
		if !isHTTPS(s.URL) {
			p.addf("%s: url %q is not https", s.label(), s.URL)
		}
	}
	return p
}

// checkSponsorMonths: since and until are months, and a sponsorship does not end before it starts.
func checkSponsorMonths(sponsors []sponsor) []string {
	var p findings
	for _, s := range sponsors {
		sinceOK := isMonth(s.Since)
		if !sinceOK {
			p.addf("%s: since %q is not a month (YYYY-MM)", s.label(), s.Since)
		}
		if s.Until == nil {
			continue
		}
		switch {
		case !isMonth(*s.Until):
			p.addf("%s: until %q is not a month (YYYY-MM)", s.label(), *s.Until)
		case sinceOK && *s.Until < s.Since:
			p.addf("%s: until before since (%s is earlier than %s)", s.label(), *s.Until, s.Since)
		}
	}
	return p
}

// checkTierCaps: at most one Title sponsor and three Lab sponsors are current in any month. A tier is fullest in a
// month where an entry starts, so counting the entries current in each start month finds every overflow.
func checkTierCaps(sponsors []sponsor) []string {
	var p findings
	for _, c := range tierCaps {
		for _, month := range startMonths(sponsors, c.tier) {
			if n := currentIn(sponsors, c.tier, month); n > c.limit {
				p.addf("%s %s in %s (at most %s)", countWord(n), c.noun, month, countWord(c.limit))
			}
		}
	}
	return p
}

// startMonths are the months in which a sponsorship of the tier starts, each once, oldest first.
func startMonths(sponsors []sponsor, tier string) []string {
	months := map[string]bool{}
	for _, s := range sponsors {
		if s.Tier == tier && validTerm(s) {
			months[s.Since] = true
		}
	}
	return slices.Sorted(maps.Keys(months))
}

// currentIn counts the sponsors of the tier whose sponsorship covers the month.
func currentIn(sponsors []sponsor, tier, month string) int {
	n := 0
	for _, s := range sponsors {
		if s.Tier == tier && validTerm(s) && covers(s.Since, s.Until, month) {
			n++
		}
	}
	return n
}

// checkMarks: a Title sponsor has no marks, a Supporter has some, and every mark points at something on the boards, so
// that a typo cannot drop a disclosure.
func checkMarks(sponsors []sponsor, f boardFacts) []string {
	var p findings
	for _, s := range sponsors {
		switch {
		case s.Tier == "title" && !s.Marks.empty():
			p.addf("%s: title with marks (a company on the boards cannot hold the Title)", s.label())
		case s.Tier == "supporter" && s.Marks.empty():
			p.addf("%s: supporter without marks (Supporter is for a company on the boards)", s.label())
		}
		for _, machine := range s.Marks.Machines {
			if !f.Machines[machine] {
				p.addf("%s: machine not on the boards: %q", s.label(), machine)
			}
		}
		for _, prefix := range s.Marks.Models {
			if !anyStartsWith(f.Models, prefix) {
				p.addf("%s: model prefix matches no row: %q", s.label(), prefix)
			}
		}
		for _, prefix := range s.Marks.Engines {
			if !anyStartsWith(f.Engines, prefix) {
				p.addf("%s: engine prefix matches no row: %q", s.label(), prefix)
			}
		}
	}
	return p
}

// checkLogoPresence: a Title, Lab sponsor or Team sponsor has its logo file; the other tiers are shown without one.
func checkLogoPresence(sponsors []sponsor, logos map[string]bool) []string {
	var p findings
	for _, s := range sponsors {
		if needsLogo(s.Tier) && !logos[s.ID] {
			p.addf("%s: no logo for %s (docs/evals/sponsors/%s.svg is missing)", s.label(), s.Tier, s.ID)
		}
	}
	return p
}

// checkPaidRuns: a run id is a slug and belongs to one run, its month is a month, and a run that names its buyer
// carries the buyer's https link (a link given without a buyer must be https too).
func checkPaidRuns(runs []run) []string {
	var p findings
	seen := map[string]bool{}
	for _, r := range runs {
		if !isSlug(r.ID) {
			p.addf("%s: id is not a slug (lowercase letters, digits and hyphens)", r.label())
		}
		if seen[r.ID] {
			p.addf("duplicate run id %q", r.ID)
		}
		seen[r.ID] = true
		if !isMonth(r.Month) {
			p.addf("%s: run month %q is not a month (YYYY-MM)", r.label(), r.Month)
		}
		if (r.By != nil || r.URL != nil) && !isHTTPS(orEmpty(r.URL)) {
			p.addf("%s: url %q is not https", r.label(), orEmpty(r.URL))
		}
	}
	return p
}

// checkSponsorsText: plain hyphens and straight quotes in every text of the sponsors and the runs.
func checkSponsorsText(doc sponsorsDoc) []string {
	var p findings
	for _, s := range doc.Sponsors {
		p.plain(s.label(), stringsOf(s))
	}
	for _, r := range doc.Runs {
		p.plain(r.label(), stringsOf(r))
	}
	return p
}

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
		edit   func(d *sponsorsDoc)
	}{
		{name: "a clean file"},
		{name: "a repeated sponsor id", want: "duplicate id", edit: func(d *sponsorsDoc) {
			d.Sponsors = append(d.Sponsors, d.Sponsors[hoursCo])
		}},
		{name: "a sponsor id that is not a slug", want: "not a slug", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].ID = "Hours Co"
		}},
		{name: "an unknown tier", want: "unknown tier", edit: func(d *sponsorsDoc) {
			d.Sponsors[hoursCo].Tier = "gold"
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
		{name: "a model bought again in a later month", edit: func(d *sponsorsDoc) {
			d.Runs = append(d.Runs, run{ID: "run-three", Model: "qwen3.8:27b", Month: "2027-01"})
		}},
		{name: "prefixes ignore case", edit: func(d *sponsorsDoc) {
			d.Sponsors[labCo].Marks = marks{Models: []string{"Qwen"}, Engines: []string{"ollama"}}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := cleanDoc()
			if c.edit != nil {
				c.edit(&doc)
			}
			got := checkSponsors(doc, testFacts(), logosOf(doc, c.noLogo))
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("want no problem, got %q", got)
			case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
				t.Errorf("want exactly one problem containing %q, got %q", c.want, got)
			}
		})
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
		got := checkSponsors(sponsorsDoc{Schema: 1, Sponsors: []sponsor{s}}, testFacts(), map[string]bool{})
		switch {
		case c.wants && (len(got) != 1 || !strings.Contains(got[0], "no logo for "+c.tier)):
			t.Errorf("a %s sponsor without a logo gave %q, want one problem about the logo", c.tier, got)
		case !c.wants && len(got) != 0:
			t.Errorf("a %s sponsor without a logo gave %q, want none", c.tier, got)
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
// boards, passes every rule, so a pull request that breaks one fails in CI before the site reads the file.
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
	for _, problem := range checkSponsors(doc, loadBoardFacts(t), logos) {
		t.Error(problem)
	}
}
