package docscheck

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// labSponsors is the sponsors file that the lab-log fixtures point at: one sponsor per tier, in the order of the
// position constants of sponsors_test.go (titleCo to boardCo), and two paid runs. All five sponsors are current in
// 2026-10: hours-co starts in that month and team-co runs on until 2027-02. Every call returns a fresh copy.
func labSponsors() sponsorsDoc {
	return sponsorsDoc{
		Schema: 1,
		Sponsors: []sponsor{
			{ID: "title-co", Name: "Title Co", Tier: "title", URL: "https://title.example", Since: "2026-09"},
			{ID: "lab-co", Name: "Lab Co", Tier: "lab-sponsor", URL: "https://lab.example", Since: "2026-09"},
			{ID: "team-co", Name: "Team Co", Tier: "team", URL: "https://team.example", Since: "2026-09",
				Until: strPtr("2027-02")},
			{ID: "hours-co", Name: "Hours Co", Tier: "lab-hours", URL: "https://github.com/hours-co", Since: "2026-10"},
			{ID: "board-co", Name: "Board Co", Tier: "supporter", URL: "https://board.example", Since: "2026-09",
				Marks: marks{Models: []string{"glm"}}},
		},
		Runs: []run{
			{ID: "run-one", Model: "qwen3.8:27b", Month: "2026-10", By: strPtr("Board Co"),
				URL: strPtr("https://board.example")},
			{ID: "run-two", Model: "gemma4:12b", Month: "2026-11"},
			{ID: "run-old", Model: "glm-4.7-flash", Month: "2026-09"},
		},
	}
}

// cleanEntry is the lab log of 2026-10 as a month that breaks no rule holds it: every list filled, every id resolving
// in labSponsors, every machine on the boards of testFacts. Every call returns a fresh copy for a case to break.
func cleanEntry() labEntry {
	return labEntry{
		Month: "2026-10", Published: "2026-10-31", Hours: 32, BoardRuns: 4, TaskRuns: 120,
		Models: []modelChange{
			{Model: "qwen3.8:27b", Change: "added", Machines: []string{"Apple M4 Max (48 GB)"}},
			{Model: "gemma4:12b", Change: "re-measured", Machines: []string{"NVIDIA RTX 5090 (32 GB)", "Apple M4 Max (48 GB)"}},
		},
		Releases: []release{
			{Project: "taraengine", Version: "0.3.1", Date: "2026-10-15", Effect: "A faster default."},
			{Project: "taracode", Version: "3.3.2", Date: "2026-10-17", Effect: "The board runs on more machines."},
		},
		Sponsors: entrySponsors{Title: []string{"title-co"}, LabSponsor: []string{"lab-co"}, Team: []string{"team-co"},
			LabHours: []string{"hours-co"}, Supporter: []string{"board-co"}, Supporters: 3},
		Runs: []string{"run-one"}, GitHubTotalUSD: 156.5, Next: "Two more models on both machines.",
	}
}

// cleanLabLog is a lab log with the one clean entry.
func cleanLabLog() labLogDoc { return labLogDoc{Schema: 1, Entries: []labEntry{cleanEntry()}} }

// bareEntry is a valid entry for the month with nothing in its lists, to stand beside the clean entry.
func bareEntry(month string) labEntry {
	return labEntry{Month: month, Published: month + "-28", Next: "More of the same."}
}

// TestCheckLabLogCatchesEachRule: a clean log gives no problems; every case breaks one rule and gets exactly one
// message, carrying the rule's key. The cases without a key are the edges that must stay clean.
func TestCheckLabLogCatchesEachRule(t *testing.T) {
	cases := []struct {
		name     string
		want     string               // the key the one message carries; empty for a log that stays clean
		log      func(l *labLogDoc)   // an edit of the lab log; the clean entry is its first
		sponsors func(s *sponsorsDoc) // an edit of the sponsors file the entries point at
	}{
		{name: "a clean entry"},
		{name: "a schema the site does not read", want: "schema", log: func(l *labLogDoc) { l.Schema = 2 }},
		{name: "a missing schema key, which reads as 0", want: "schema", log: func(l *labLogDoc) { l.Schema = 0 }},

		// The run, bought in 2026-10, is left out here: it would also miss an entry of another month.
		{name: "a month that is not a month", want: "not a month", log: func(l *labLogDoc) {
			l.Entries[0].Month, l.Entries[0].Runs = "2026-1", nil
		}},
		{name: "a month listed twice", want: "duplicate month", log: func(l *labLogDoc) {
			l.Entries = append(l.Entries, bareEntry("2026-10"))
		}},
		{name: "a newer month after an older one", want: "not newest first", log: func(l *labLogDoc) {
			l.Entries = append(l.Entries, bareEntry("2026-11"))
		}},
		{name: "an older month after a newer one", log: func(l *labLogDoc) {
			l.Entries = append(l.Entries, bareEntry("2026-09"))
		}},
		{name: "a later entry with an impossible month", want: "not a month", log: func(l *labLogDoc) {
			e := bareEntry("2026-09")
			e.Month = "2026-13"
			l.Entries = append(l.Entries, e)
		}},

		{name: "published that is not a date", want: "published is not a date", log: func(l *labLogDoc) {
			l.Entries[0].Published = "2026-10-32"
		}},
		{name: "published on a leap day", log: func(l *labLogDoc) { l.Entries[0].Published = "2028-02-29" }},

		{name: "negative hours", want: "negative", log: func(l *labLogDoc) { l.Entries[0].Hours = -1 }},
		{name: "negative board runs", want: "negative", log: func(l *labLogDoc) { l.Entries[0].BoardRuns = -1 }},
		{name: "negative task runs", want: "negative", log: func(l *labLogDoc) { l.Entries[0].TaskRuns = -1 }},
		{name: "a negative count of supporters", want: "negative", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Supporters = -1
		}},
		{name: "a month with nothing to count", log: func(l *labLogDoc) {
			e := &l.Entries[0]
			e.Hours, e.BoardRuns, e.TaskRuns, e.Sponsors.Supporters, e.GitHubTotalUSD = 0, 0, 0, 0, 0
		}},

		{name: "a change that is neither added nor re-measured", want: "change must be added or re-measured",
			log: func(l *labLogDoc) { l.Entries[0].Models[1].Change = "updated" }},
		{name: "a machine that is on no board", want: "machine not on the boards", log: func(l *labLogDoc) {
			l.Entries[0].Models[0].Machines = []string{"NVIDIA RTX 4090 (24 GB)"}
		}},
		{name: "an empty machine label", want: "machine not on the boards", log: func(l *labLogDoc) {
			l.Entries[0].Models[0].Machines = []string{""}
		}},
		{name: "a machine label in another case", want: "machine not on the boards", log: func(l *labLogDoc) {
			l.Entries[0].Models[0].Machines = []string{"apple m4 max (48 gb)"}
		}},
		{name: "a machine label cut short, the second of the second model", want: "machine not on the boards",
			log: func(l *labLogDoc) { l.Entries[0].Models[1].Machines[1] = "Apple M4 Max" }},

		{name: "a project that is neither taraengine nor taracode", want: "project must be taraengine or taracode",
			log: func(l *labLogDoc) { l.Entries[0].Releases[0].Project = "ollama" }},
		{name: "a project in another case", want: "project must be taraengine or taracode",
			log: func(l *labLogDoc) { l.Entries[0].Releases[1].Project = "Taracode" }},
		{name: "a release date that is not a date", want: "release date", log: func(l *labLogDoc) {
			l.Entries[0].Releases[1].Date = "2026-13-01"
		}},

		{name: "a sponsor id that is not in sponsors.json", want: "unknown sponsor", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Team = []string{"nobody-co"}
		}},
		{name: "a sponsor listed under the wrong tier", want: "not team in 2026-10", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Team = []string{"hours-co"}
		}},
		// hours-co is taken off the lab-hours list, so that the wrong-tier listing is its only one.
		{name: "a sponsor of another tier whose sponsorship has also ended", want: "not team in 2026-10",
			log: func(l *labLogDoc) {
				e := &l.Entries[0]
				e.Sponsors.Team, e.Sponsors.LabHours = []string{"hours-co"}, nil
			},
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors[hoursCo].Since, s.Sponsors[hoursCo].Until = "2026-08", strPtr("2026-09")
			}},
		{name: "a sponsorship that ended before the month", want: "not current in 2026-10", sponsors: func(s *sponsorsDoc) {
			s.Sponsors[teamCo].Until = strPtr("2026-09")
		}},
		{name: "a sponsorship that starts after the month", want: "not current in 2026-10", sponsors: func(s *sponsorsDoc) {
			s.Sponsors[hoursCo].Since = "2026-11"
		}},
		{name: "a sponsorship that ends in the month", sponsors: func(s *sponsorsDoc) {
			s.Sponsors[teamCo].Until = strPtr("2026-10")
		}},
		{name: "a sponsorship that starts in the month", sponsors: func(s *sponsorsDoc) {
			s.Sponsors[labCo].Since = "2026-10"
		}},
		{name: "a first month that is not a month is left to the sponsors file", sponsors: func(s *sponsorsDoc) {
			s.Sponsors[hoursCo].Since = "2026-13"
		}},

		{name: "a run that is not in sponsors.json", want: "unknown run", log: func(l *labLogDoc) {
			l.Entries[0].Runs = []string{"run-zzz"}
		}},
		{name: "a run bought in a later month", want: "run bought in another month", log: func(l *labLogDoc) {
			l.Entries[0].Runs = []string{"run-two"}
		}},
		{name: "a run bought in an earlier month", want: "run bought in another month", log: func(l *labLogDoc) {
			l.Entries[0].Runs = []string{"run-old"}
		}},

		{name: "a negative GitHub total", want: "negative total", log: func(l *labLogDoc) {
			l.Entries[0].GitHubTotalUSD = -1
		}},
		{name: "no word on the next month", want: "empty next", log: func(l *labLogDoc) { l.Entries[0].Next = "" }},
		{name: "a next of spaces", want: "empty next", log: func(l *labLogDoc) { l.Entries[0].Next = "  " }},

		{name: "a fancy dash in next", want: "fancy character", log: func(l *labLogDoc) {
			l.Entries[0].Next = "Two models " + enDash + " one machine."
		}},
		{name: "a curly quote in a release effect", want: "fancy character", log: func(l *labLogDoc) {
			l.Entries[0].Releases[0].Effect = "The lab" + string(rune(0x2019)) + "s new default."
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, sponsors := cleanLabLog(), labSponsors()
			if c.log != nil {
				c.log(&log)
			}
			if c.sponsors != nil {
				c.sponsors(&sponsors)
			}
			got := checkLabLog(log, sponsors, testFacts())
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("want no problem, got %q", got)
			case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
				t.Errorf("want exactly one problem containing %q, got %q", c.want, got)
			}
		})
	}
}

// tierSetters set the list an entry keeps for one tier, in the order of knownTiers.
var tierSetters = []struct {
	tier string
	set  func(s *entrySponsors, ids []string)
}{
	{"title", func(s *entrySponsors, ids []string) { s.Title = ids }},
	{"lab-sponsor", func(s *entrySponsors, ids []string) { s.LabSponsor = ids }},
	{"team", func(s *entrySponsors, ids []string) { s.Team = ids }},
	{"lab-hours", func(s *entrySponsors, ids []string) { s.LabHours = ids }},
	{"supporter", func(s *entrySponsors, ids []string) { s.Supporter = ids }},
}

// idOfTier is the id of the sponsor of the document that holds the tier.
func idOfTier(d sponsorsDoc, tier string) string {
	for _, s := range d.Sponsors {
		if s.Tier == tier {
			return s.ID
		}
	}
	return ""
}

// TestEveryTierListOfAnEntryIsChecked: each of the five lists is read, and a message names the list it found an id in
// and, for a sponsor of another tier, the tier that list stands for.
func TestEveryTierListOfAnEntryIsChecked(t *testing.T) {
	tiers := make([]string, 0, len(tierSetters))
	for _, c := range tierSetters {
		tiers = append(tiers, c.tier)
	}
	if !slices.Equal(tiers, knownTiers) {
		t.Fatalf("an entry has lists for the tiers %q, but the design has the tiers %q", tiers, knownTiers)
	}
	for i, c := range tierSetters {
		other := idOfTier(labSponsors(), tierSetters[(i+1)%len(tierSetters)].tier)
		for _, k := range []struct{ name, id, want string }{
			{"an id that is not in sponsors.json", "nobody-co", "unknown sponsor"},
			{"a sponsor of another tier", other, "not " + c.tier + " in 2026-10"},
		} {
			t.Run(c.tier+": "+k.name, func(t *testing.T) {
				log := cleanLabLog()
				c.set(&log.Entries[0].Sponsors, []string{k.id})
				got := checkLabLog(log, labSponsors(), testFacts())
				if len(got) != 1 || !strings.Contains(got[0], k.want) || !strings.Contains(got[0], "sponsors."+c.tier+":") {
					t.Errorf("want exactly one problem containing %q for the list %q, got %q", k.want, c.tier, got)
				}
			})
		}
	}
}

// TestIsDateWantsAnExistingDayWrittenYYYYMMDD: a date is a day on the calendar, leap days included, written with two
// digits for the month and the day and nothing around it.
func TestIsDateWantsAnExistingDayWrittenYYYYMMDD(t *testing.T) {
	for _, s := range []string{"2026-10-31", "2026-01-01", "2026-12-31", "2028-02-29"} {
		if !isDate(s) {
			t.Errorf("isDate(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "2026-10", "2026-10-32", "2026-10-00", "2026-00-10", "2026-13-01", "2026-02-29",
		"2026-02-30", "2026-1-5", "2026-10-3", "26-10-31", "2026/10/31", "31-10-2026", " 2026-10-31", "2026-10-31 ",
		"2026-10-31T00:00:00Z"} {
		if isDate(s) {
			t.Errorf("isDate(%q) = true, want false", s)
		}
	}
}

// designLog is a lab log written with the keys of the design's example, filled with the values of cleanEntry. It is
// written apart from the types, so that their json tags are pinned by a text of their own: the committed file has no
// entry yet, and a misspelt tag would first show when the first month is published.
const designLog = `{
  "schema": 1,
  "entries": [{
    "month": "2026-10",
    "published": "2026-10-31",
    "hours": 32,
    "board_runs": 4,
    "task_runs": 120,
    "models": [
      {"model": "qwen3.8:27b", "change": "added", "machines": ["Apple M4 Max (48 GB)"]},
      {"model": "gemma4:12b", "change": "re-measured", "machines": ["NVIDIA RTX 5090 (32 GB)", "Apple M4 Max (48 GB)"]}
    ],
    "releases": [
      {"project": "taraengine", "version": "0.3.1", "date": "2026-10-15", "effect": "A faster default."},
      {"project": "taracode", "version": "3.3.2", "date": "2026-10-17", "effect": "The board runs on more machines."}
    ],
    "sponsors": {"title": ["title-co"], "lab-sponsor": ["lab-co"], "team": ["team-co"], "lab-hours": ["hours-co"],
      "supporter": ["board-co"], "supporters": 3},
    "runs": ["run-one"],
    "github_total_usd": 156.5,
    "next": "Two more models on both machines."
  }]
}`

// TestLabLogReadsTheKeysOfTheDesign: the design's keys decode into the fields of the types, and what they decode to
// passes the rules.
func TestLabLogReadsTheKeysOfTheDesign(t *testing.T) {
	var doc labLogDoc
	if err := decodeStrict([]byte(designLog), &doc); err != nil {
		t.Fatal(err)
	}
	if want := cleanLabLog(); !reflect.DeepEqual(doc, want) {
		t.Errorf("decoded %+v\nwant    %+v", doc, want)
	}
	for _, problem := range checkLabLog(doc, labSponsors(), testFacts()) {
		t.Error(problem)
	}
}

// TestLabLogRefusesAmountsAndUnknownKeys: the lab log carries no amount but the month's GitHub total, and no key the
// types leave out. An amount beside a sponsor, a model or a release, or kept among the sponsors, fails to decode, as do
// hours that are not whole.
func TestLabLogRefusesAmountsAndUnknownKeys(t *testing.T) {
	for _, c := range []struct {
		name, data string
		ok         bool
	}{
		{"an entry with a month only", `{"schema": 1, "entries": [{"month": "2026-10"}]}`, true},
		{"an amount on an entry", `{"schema": 1, "entries": [{"month": "2026-10", "amount": 30}]}`, false},
		{"an amount beside a sponsor id",
			`{"schema": 1, "entries": [{"sponsors": {"team": [{"id": "team-co", "amount": 30}]}}]}`, false},
		{"an amount among the sponsors", `{"schema": 1, "entries": [{"sponsors": {"total": 30}}]}`, false},
		{"an amount on a model change", `{"schema": 1, "entries": [{"models": [{"model": "m", "cost": 30}]}]}`, false},
		{"an amount on a release", `{"schema": 1, "entries": [{"releases": [{"project": "taracode", "price": 30}]}]}`, false},
		{"hours with a fraction", `{"schema": 1, "entries": [{"hours": 1.5}]}`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var doc labLogDoc
			if err := decodeStrict([]byte(c.data), &doc); (err == nil) != c.ok {
				t.Errorf("decodeStrict(%s) error = %v, want an error: %v", c.data, err, !c.ok)
			}
		})
	}
}

// TestLabLogSchemaOfAFileIsChecked: a file that declares schema 2, or declares none (a missing key decodes to 0), is
// refused, since the site takes it as invalid and then shows the lab log's invitation instead of its entries.
func TestLabLogSchemaOfAFileIsChecked(t *testing.T) {
	for _, c := range []struct {
		name, data string
		refused    bool
	}{
		{"schema 1", `{"schema": 1, "entries": []}`, false},
		{"schema 2", `{"schema": 2, "entries": []}`, true},
		{"no schema key", `{"entries": []}`, true},
	} {
		var doc labLogDoc
		if err := decodeStrict([]byte(c.data), &doc); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := checkLabLog(doc, labSponsors(), testFacts())
		switch {
		case c.refused && (len(got) != 1 || !strings.Contains(got[0], "schema")):
			t.Errorf("%s: problems = %q, want exactly one about the schema", c.name, got)
		case !c.refused && len(got) != 0:
			t.Errorf("%s: problems = %q, want none", c.name, got)
		}
	}
}

// The committed lab log.
const labLogPath = docsDir + "lab-log.json"

// TestLabLogFileFollowsTheRules: the committed lab-log.json, with the committed sponsors.json its ids point at and the
// committed boards its machines are on, passes every rule, so a pull request that breaks one fails in CI before the
// site reads the file.
func TestLabLogFileFollowsTheRules(t *testing.T) {
	var log labLogDoc
	readJSON(t, labLogPath, &log)
	var sponsors sponsorsDoc
	readJSON(t, sponsorsPath, &sponsors)
	for _, problem := range checkLabLog(log, sponsors, loadBoardFacts(t)) {
		t.Error(problem)
	}
}
