package docscheck

import (
	"cmp"
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

// logMonth is the month the lab-log fixtures are checked in, as the current month: the month of cleanEntry.
const logMonth = "2026-10"

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
		today    string               // the current month the log is checked in; empty for logMonth
		log      func(l *labLogDoc)   // an edit of the lab log; the clean entry is its first
		sponsors func(s *sponsorsDoc) // an edit of the sponsors file the entries point at
	}{
		{name: "a clean entry"},
		{name: "a schema the site does not read", want: "schema", log: func(l *labLogDoc) { l.Schema = 2 }},
		{name: "a schema of 0", want: "schema", log: func(l *labLogDoc) { l.Schema = 0 }},

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
		// The machines of the entries of the current and the previous month are held to the boards; an older entry
		// keeps the machines it named, so that a board refresh cannot fail an old record.
		{name: "a machine gone from the boards in last month's entry", want: "machine not on the boards",
			today: "2026-11", log: func(l *labLogDoc) {
				l.Entries[0].Models[0].Machines = []string{"NVIDIA RTX 4090 (24 GB)"}
			}},
		{name: "a machine gone from the boards in an older entry", today: "2026-12", log: func(l *labLogDoc) {
			l.Entries[0].Models[0].Machines = []string{"NVIDIA RTX 4090 (24 GB)"}
		}},
		{name: "a machine not on the boards in an entry for a later month", want: "machine not on the boards",
			today: "2026-09", log: func(l *labLogDoc) {
				l.Entries[0].Models[0].Machines = []string{"NVIDIA RTX 4090 (24 GB)"}
			}},

		{name: "a project that is neither taraengine nor taracode", want: "project must be taraengine or taracode",
			log: func(l *labLogDoc) { l.Entries[0].Releases[0].Project = "ollama" }},
		{name: "a project in another case", want: "project must be taraengine or taracode",
			log: func(l *labLogDoc) { l.Entries[0].Releases[1].Project = "Taracode" }},
		{name: "a release date that is not a date", want: "release date", log: func(l *labLogDoc) {
			l.Entries[0].Releases[1].Date = "2026-13-01"
		}},

		// The page shows every model, version and effect, and counts what the lists hold.
		{name: "a model change without a model", want: "empty model", log: func(l *labLogDoc) {
			l.Entries[0].Models[1].Model = ""
		}},
		{name: "a model of spaces", want: "empty model", log: func(l *labLogDoc) { l.Entries[0].Models[1].Model = "  " }},
		{name: "a model change on no machine", want: "no machines", log: func(l *labLogDoc) {
			l.Entries[0].Models[0].Machines = []string{}
		}},
		{name: "a release without a version", want: "empty version", log: func(l *labLogDoc) {
			l.Entries[0].Releases[0].Version = ""
		}},
		{name: "a version of spaces", want: "empty version", log: func(l *labLogDoc) {
			l.Entries[0].Releases[0].Version = "  "
		}},
		{name: "a release without an effect", want: "empty effect", log: func(l *labLogDoc) {
			l.Entries[0].Releases[1].Effect = ""
		}},
		{name: "an effect of spaces", want: "empty effect", log: func(l *labLogDoc) {
			l.Entries[0].Releases[1].Effect = "  "
		}},
		{name: "a sponsor listed twice in one list", want: "listed twice", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Team = []string{"team-co", "team-co"}
		}},
		{name: "a run listed twice", want: "listed twice", log: func(l *labLogDoc) {
			l.Entries[0].Runs = []string{"run-one", "run-one"}
		}},
		{name: "a model change listed twice", want: "listed twice", log: func(l *labLogDoc) {
			l.Entries[0].Models = append(l.Entries[0].Models, l.Entries[0].Models[0])
		}},
		{name: "a model change listed twice with other machines", want: "listed twice", log: func(l *labLogDoc) {
			l.Entries[0].Models = append(l.Entries[0].Models,
				modelChange{Model: "qwen3.8:27b", Change: "added", Machines: []string{"NVIDIA RTX 5090 (32 GB)"}})
		}},
		{name: "a model added and measured again in one month", log: func(l *labLogDoc) {
			l.Entries[0].Models = append(l.Entries[0].Models,
				modelChange{Model: "qwen3.8:27b", Change: "re-measured", Machines: []string{"Apple M4 Max (48 GB)"}})
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
			got := checkLabLog(log, sponsors, testFacts(), cmp.Or(c.today, logMonth))
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("want no problem, got %q", got)
			case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
				t.Errorf("want exactly one problem containing %q, got %q", c.want, got)
			}
		})
	}
}

// TestLabLogNamesWhatAListHoldsTwice: a message names the entry, the list and the id, or the model and the change, that
// stand twice; three of one are one message, and each list is judged on its own.
func TestLabLogNamesWhatAListHoldsTwice(t *testing.T) {
	log := cleanLabLog()
	e := &log.Entries[0]
	e.Sponsors.Team = []string{"team-co", "team-co", "team-co"}
	e.Runs = []string{"run-one", "run-one"}
	e.Models = append(e.Models, e.Models[1])
	want := []string{
		`entry "2026-10", model "gemma4:12b": change "re-measured" listed twice (list each change of a model once, ` +
			`with all its machines)`,
		`entry "2026-10": sponsors.team: "team-co" listed twice`,
		`entry "2026-10": runs: "run-one" listed twice`,
	}
	if got := checkLabLog(log, labSponsors(), testFacts(), logMonth); !slices.Equal(got, want) {
		t.Errorf("checkLabLog = %q\nwant %q", got, want)
	}
}

// TestLabLogNamesTheModelAndTheRelease: a message about a model change names the entry and the model, and one about a
// release names the entry, the project and the version in quotes, so that an empty one still reads as empty.
func TestLabLogNamesTheModelAndTheRelease(t *testing.T) {
	log := cleanLabLog()
	log.Entries[0].Models[1].Model, log.Entries[0].Releases[0].Version = "", ""
	want := []string{
		`entry "2026-10", model "": empty model (name the model as the boards do)`,
		`entry "2026-10", release taraengine "": empty version (give the version that was released)`,
	}
	if got := checkLabLog(log, labSponsors(), testFacts(), logMonth); !slices.Equal(got, want) {
		t.Errorf("checkLabLog = %q\nwant %q", got, want)
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
				got := checkLabLog(log, labSponsors(), testFacts(), logMonth)
				if len(got) != 1 || !strings.Contains(got[0], k.want) || !strings.Contains(got[0], "sponsors."+c.tier+":") {
					t.Errorf("want exactly one problem containing %q for the list %q, got %q", k.want, c.tier, got)
				}
			})
		}
	}
}

// TestBeforeLastMonthLetsGoOnlyWhatIsPast: a month is past once it is older than the month before the current one,
// across a year too. The previous month, the current one, a later one and anything that is not a month are not.
func TestBeforeLastMonthLetsGoOnlyWhatIsPast(t *testing.T) {
	for _, c := range []struct {
		month, thisMonth string
		want             bool
	}{
		{"2026-08", "2026-10", true},
		{"2026-09", "2026-10", false},
		{"2026-10", "2026-10", false},
		{"2026-11", "2026-10", false},
		{"2026-11", "2027-01", true},
		{"2026-12", "2027-01", false},
		{"2026-1", "2026-10", false},
		{"2025-13", "2026-10", false},
	} {
		if got := beforeLastMonth(c.month, c.thisMonth); got != c.want {
			t.Errorf("beforeLastMonth(%q, %q) = %v, want %v", c.month, c.thisMonth, got, c.want)
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
	for _, problem := range checkLabLog(doc, labSponsors(), testFacts(), logMonth) {
		t.Error(problem)
	}
}

// TestLabLogRefusesAmountsAndUnknownKeys: the lab log carries no amount but the month's GitHub total, and no key the
// types leave out. An amount on an entry, a model change or a release, or kept among the sponsors, fails to decode, as
// do a sponsor id given as an object with an amount and hours that are not whole. Each case edits the design's lab
// log, which holds every key, so that it fails for the amount alone.
func TestLabLogRefusesAmountsAndUnknownKeys(t *testing.T) {
	checkDecodeCases(t, designLog, func() any { return &labLogDoc{} }, []decodeCase{
		{"an amount on an entry", `"hours": 32,`, `"hours": 32, "amount": 30,`, `entries[0]: unknown key "amount"`},
		{"a sponsor id given as an object with an amount", `"team": ["team-co"]`,
			`"team": [{"id": "team-co", "amount": 30}]`, "entries[0].sponsors.team[0]: an object, but the rules want a text"},
		{"an amount among the sponsors", `"supporters": 3`, `"supporters": 3, "total": 30`,
			`entries[0].sponsors: unknown key "total"`},
		{"an amount on a model change", `"change": "added",`, `"change": "added", "cost": 30,`,
			`entries[0].models[0]: unknown key "cost"`},
		{"an amount on a release", `"version": "0.3.1",`, `"version": "0.3.1", "price": 30,`,
			`entries[0].releases[0]: unknown key "price"`},
		{"hours with a fraction", `"hours": 32`, `"hours": 1.5`, "number 1.5"},
	})
}

// TestLabLogSchemaOfAFileIsChecked: a file that declares schema 2 or 0 is refused by the rules, and one that declares
// none by the decoding, since the site takes either as invalid and then shows the lab log's invitation instead of its
// entries.
func TestLabLogSchemaOfAFileIsChecked(t *testing.T) {
	for _, c := range []struct{ name, data, want string }{
		{"schema 1", `{"schema": 1, "entries": []}`, ""},
		{"schema 2", `{"schema": 2, "entries": []}`, "schema is 2"},
		{"schema 0", `{"schema": 0, "entries": []}`, "schema is 0"},
		{"no schema key", `{"entries": []}`, `missing key "schema"`},
	} {
		var doc labLogDoc
		got := problemsOf(c.data, &doc, func() []string { return checkLabLog(doc, labSponsors(), testFacts(), logMonth) })
		switch {
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
			t.Errorf("%s: problems = %q, want exactly one containing %q", c.name, got, c.want)
		case c.want == "" && len(got) != 0:
			t.Errorf("%s: problems = %q, want none", c.name, got)
		}
	}
}

// The committed lab log.
const labLogPath = docsDir + "lab-log.json"

// TestLabLogFileFollowsTheRules: the committed lab-log.json, within the size the site reads, with the committed
// sponsors.json its ids point at and the committed boards its machines are on, passes every rule in the current month,
// so a pull request that breaks one fails in CI before the site reads the file.
func TestLabLogFileFollowsTheRules(t *testing.T) {
	var log labLogDoc
	data := readJSON(t, labLogPath, &log)
	var sponsors sponsorsDoc
	readJSON(t, sponsorsPath, &sponsors)
	problems := slices.Concat(checkFileSize(labLogPath, data, labLogMaxBytes),
		checkLabLog(log, sponsors, loadBoardFacts(t), currentMonth()))
	for _, problem := range problems {
		t.Error(problem)
	}
}
