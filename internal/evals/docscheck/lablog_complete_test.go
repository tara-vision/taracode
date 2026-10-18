package docscheck

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// inTerm reports whether the sponsorship is known to cover the month, which must be a month. Like outsideTerm it judges
// only a term made of months: the sponsors file reports a malformed one.
func inTerm(s sponsor, month string) bool {
	return validTerm(s) && covers(s.Since, s.Until, month)
}

// checkLabLogComplete checks that an entry leaves nothing out, which checkLabLog does not: that one checks what an
// entry lists. Every sponsor of sponsors.json whose credit covers the month of an entry is on the entry's list for its
// tier, so that a Lab sponsor keeps its credit in every lab log and a board company is named in it whatever it paid;
// and every paid run bought in that month is on the entry's runs. An entry whose month is not a month is left to
// checkLabLogMonths. The two functions run on their own, so that a case of either table breaks one rule only.
func checkLabLogComplete(doc labLogDoc, s sponsorsDoc) []string {
	withMonth := slices.DeleteFunc(slices.Clone(doc.Entries), func(e labEntry) bool { return !isMonth(e.Month) })
	return slices.Concat(
		checkLabLogSponsorsListed(withMonth, s.Sponsors),
		checkLabLogRunsListed(withMonth, s.Runs),
	)
}

// checkLabLogSponsorsListed: each sponsor current in the month of an entry is on the list the entry keeps for its tier.
// A sponsor of a tier that has no list is left to the sponsors file, which reports the tier.
func checkLabLogSponsorsListed(entries []labEntry, sponsors []sponsor) []string {
	var p findings
	for _, e := range entries {
		for _, l := range e.Sponsors.lists() {
			for _, s := range sponsors {
				if s.Tier == l.tier && inTerm(s, e.Month) && !slices.Contains(l.ids, s.ID) {
					p.addf("%s: sponsor not listed: %q is current in %s but is missing from sponsors.%s",
						e.label(), s.ID, e.Month, l.tier)
				}
			}
		}
	}
	return p
}

// checkLabLogRunsListed: each paid run bought in the month of an entry is on the entry's runs.
func checkLabLogRunsListed(entries []labEntry, runs []run) []string {
	var p findings
	for _, e := range entries {
		for _, r := range runs {
			if r.Month == e.Month && !slices.Contains(e.Runs, r.ID) {
				p.addf("%s: run not listed: %q was bought in %s but is missing from runs", e.label(), r.ID, e.Month)
			}
		}
	}
	return p
}

// olderEntry is a complete lab log for 2026-09: the sponsors of labSponsors that are current that month (hours-co
// starts a month later) and the paid run bought then.
func olderEntry() labEntry {
	e := bareEntry("2026-09")
	e.Sponsors = entrySponsors{Title: []string{"title-co"}, LabSponsor: []string{"lab-co"}, Team: []string{"team-co"},
		Supporter: []string{"board-co"}}
	e.Runs = []string{"run-old"}
	return e
}

// TestCheckLabLogCompleteCatchesEachOmission: a log that lists everything its months hold gives no problems; every case
// leaves one thing out and gets exactly one message, carrying the key. The cases without a key are the edges that must
// stay clean: a sponsor or a run outside the month need not be listed.
func TestCheckLabLogCompleteCatchesEachOmission(t *testing.T) {
	cases := []struct {
		name     string
		want     string               // the key the one message carries; empty for a log that stays clean
		log      func(l *labLogDoc)   // an edit of the lab log; the clean entry is its first
		sponsors func(s *sponsorsDoc) // an edit of the sponsors file the entries are held against
	}{
		{name: "an entry listing everything"},
		{name: "an older entry listing its own month", log: func(l *labLogDoc) {
			l.Entries = append(l.Entries, olderEntry())
		}},

		// In the next five cases the sponsor or the run is off the entry's list as well, so that a rule that wrongly
		// asked for it would have something to report.
		{name: "a sponsor whose first month is after the month",
			log: func(l *labLogDoc) { l.Entries[0].Sponsors.LabHours = nil },
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors[hoursCo].Since = "2026-11"
			}},
		{name: "a sponsor whose last month is before the month",
			log: func(l *labLogDoc) { l.Entries[0].Sponsors.Team = nil },
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors[teamCo].Until = strPtr("2026-09")
			}},
		{name: "a paid run bought in another month",
			log: func(l *labLogDoc) { l.Entries[0].Runs = nil },
			sponsors: func(s *sponsorsDoc) {
				s.Runs[0].Month = "2026-11"
			}},
		{name: "a sponsorship whose first month is not a month is left to the sponsors file",
			log: func(l *labLogDoc) { l.Entries[0].Sponsors.LabHours = nil },
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors[hoursCo].Since = "2026-1"
			}},
		{name: "a sponsor of a tier that has no list is left to the sponsors file",
			log: func(l *labLogDoc) { l.Entries[0].Sponsors.LabHours = nil },
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors[hoursCo].Tier = "gold"
			}},
		{name: "an entry whose month is not a month is left to the month rule", log: func(l *labLogDoc) {
			e := &l.Entries[0]
			e.Month, e.Sponsors.Team, e.Runs = "2026-1", nil, nil
		}},

		{name: "the Title sponsor left off", want: "sponsor not listed", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Title = nil
		}},
		{name: "the Lab sponsor left off", want: "sponsor not listed", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.LabSponsor = nil
		}},
		{name: "the Team sponsor left off", want: "sponsor not listed", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Team = nil
		}},
		{name: "the Lab hours sponsor left off, in the month it starts", want: "sponsor not listed",
			log: func(l *labLogDoc) { l.Entries[0].Sponsors.LabHours = nil }},
		{name: "the board company left off the supporter list", want: "sponsor not listed", log: func(l *labLogDoc) {
			l.Entries[0].Sponsors.Supporter = nil
		}},
		{name: "a sponsor left off in the month it ends", want: "sponsor not listed",
			log: func(l *labLogDoc) { l.Entries[0].Sponsors.Team = nil },
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors[teamCo].Until = strPtr("2026-10")
			}},
		{name: "a second sponsor left off a list that names the first", want: "sponsor not listed",
			sponsors: func(s *sponsorsDoc) {
				s.Sponsors = append(s.Sponsors, starting(another("team-two", "team"), "2026-10"))
			}},
		{name: "a sponsor listed under another tier is still missing from its own", want: "sponsor not listed",
			log: func(l *labLogDoc) {
				e := &l.Entries[0]
				e.Sponsors.Team, e.Sponsors.LabHours = nil, []string{"hours-co", "team-co"}
			}},

		{name: "a paid run left off", want: "run not listed", log: func(l *labLogDoc) { l.Entries[0].Runs = nil }},
		{name: "one of two paid runs of the month left off", want: "run not listed", sponsors: func(s *sponsorsDoc) {
			s.Runs = append(s.Runs, run{ID: "run-three", Model: "gemma4:12b", Month: "2026-10"})
		}},

		{name: "an older entry that leaves out a sponsor of its month", want: "sponsor not listed",
			log: func(l *labLogDoc) {
				older := olderEntry()
				older.Sponsors.Team = nil
				l.Entries = append(l.Entries, older)
			}},
		{name: "an older entry that leaves out its paid run", want: "run not listed", log: func(l *labLogDoc) {
			older := olderEntry()
			older.Runs = nil
			l.Entries = append(l.Entries, older)
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
			got := checkLabLogComplete(log, sponsors)
			switch {
			case c.want == "" && len(got) != 0:
				t.Errorf("want no problem, got %q", got)
			case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
				t.Errorf("want exactly one problem containing %q, got %q", c.want, got)
			}
		})
	}
}

// TestCheckLabLogCompleteNamesTheEntryTheIdAndTheList: a message says which entry, which id and which list it is
// missing from, and the sponsors' messages come before the runs'.
func TestCheckLabLogCompleteNamesTheEntryTheIdAndTheList(t *testing.T) {
	log := cleanLabLog()
	log.Entries[0].Sponsors.Team, log.Entries[0].Runs = nil, nil
	want := []string{
		`entry "2026-10": sponsor not listed: "team-co" is current in 2026-10 but is missing from sponsors.team`,
		`entry "2026-10": run not listed: "run-one" was bought in 2026-10 but is missing from runs`,
	}
	if got := checkLabLogComplete(log, labSponsors()); !slices.Equal(got, want) {
		t.Errorf("checkLabLogComplete = %q\nwant %q", got, want)
	}
}

// TestCheckLabLogCompleteLeavesItsInputsAlone: the entries whose month is not a month are dropped from a copy, so the
// lab log the caller holds keeps every entry where it was.
func TestCheckLabLogCompleteLeavesItsInputsAlone(t *testing.T) {
	log := cleanLabLog()
	bad := olderEntry()
	bad.Month = "2026-13"
	log.Entries = []labEntry{bad, log.Entries[0]}
	want := slices.Clone(log.Entries)
	checkLabLogComplete(log, labSponsors())
	if !reflect.DeepEqual(log.Entries, want) {
		t.Errorf("the entries were changed to %+v\nwant them as they were, %+v", log.Entries, want)
	}
}

// TestLabLogFileIsComplete: the committed lab-log.json lists every sponsor and paid run that the committed
// sponsors.json puts in the month of each of its entries, so a month-end entry that forgets one fails in CI before the
// site drops a credit or a disclosure.
func TestLabLogFileIsComplete(t *testing.T) {
	var log labLogDoc
	readJSON(t, labLogPath, &log)
	var sponsors sponsorsDoc
	readJSON(t, sponsorsPath, &sponsors)
	for _, problem := range checkLabLogComplete(log, sponsors) {
		t.Error(problem)
	}
}
