package docscheck

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// labLogDoc is docs/evals/lab-log.json: one entry per month, newest first.
type labLogDoc struct {
	Schema  int        `json:"schema"`
	Entries []labEntry `json:"entries"`
}

// labEntry is the public lab log of one month. GitHubTotalUSD is the one amount a lab log may hold: the month's GitHub
// Sponsors total, never an amount per sponsor.
type labEntry struct {
	Month          string        `json:"month"`
	Published      string        `json:"published"`
	Hours          int           `json:"hours"`
	BoardRuns      int           `json:"board_runs"`
	TaskRuns       int           `json:"task_runs"`
	Models         []modelChange `json:"models"`
	Releases       []release     `json:"releases"`
	Sponsors       entrySponsors `json:"sponsors"`
	Runs           []string      `json:"runs"`
	GitHubTotalUSD float64       `json:"github_total_usd"`
	Next           string        `json:"next"`
}

// modelChange is a model added to the boards, or measured again, on the machines named.
type modelChange struct {
	Model    string   `json:"model"`
	Change   string   `json:"change"`
	Machines []string `json:"machines"`
}

// release is a release of the month and what it changed for the boards.
type release struct {
	Project string `json:"project"`
	Version string `json:"version"`
	Date    string `json:"date"`
	Effect  string `json:"effect"`
}

// entrySponsors lists the month's sponsors by id, one list per tier. Supporters counts the Supporters who are not
// listed: a board company at Supporter is listed under Supporter, and any other Supporter is a number.
type entrySponsors struct {
	Title      []string `json:"title"`
	LabSponsor []string `json:"lab-sponsor"`
	Team       []string `json:"team"`
	LabHours   []string `json:"lab-hours"`
	Supporter  []string `json:"supporter"`
	Supporters int      `json:"supporters"`
}

const (
	// labLogSchema is the one version of lab-log.json that the rules and the site read.
	labLogSchema = 1
	// dateLayout is a day written YYYY-MM-DD, in the reference time of the time package.
	dateLayout = "2006-01-02"
)

var (
	// labChanges are what a lab log may say happened to a model on the boards.
	labChanges = []string{"added", "re-measured"}
	// labProjects are the projects whose releases a lab log reports.
	labProjects = []string{"taraengine", "taracode"}
)

// label names an entry in a message by its month.
func (e labEntry) label() string { return fmt.Sprintf("entry %q", e.Month) }

// modelLabel names one of the entry's model changes in a message.
func (e labEntry) modelLabel(m modelChange) string {
	return fmt.Sprintf("%s, model %q", e.label(), m.Model)
}

// releaseLabel names one of the entry's releases in a message.
func (e labEntry) releaseLabel(r release) string {
	return fmt.Sprintf("%s, %s %s", e.label(), r.Project, r.Version)
}

// isDate reports whether s is a day on the calendar, written YYYY-MM-DD.
func isDate(s string) bool {
	_, err := time.Parse(dateLayout, s)
	return err == nil
}

// tierList is the ids an entry lists for one tier.
type tierList struct {
	tier string
	ids  []string
}

// lists are the sponsor lists of an entry, each with the tier it stands for, in the order of knownTiers.
func (s entrySponsors) lists() []tierList {
	return []tierList{
		{"title", s.Title},
		{"lab-sponsor", s.LabSponsor},
		{"team", s.Team},
		{"lab-hours", s.LabHours},
		{"supporter", s.Supporter},
	}
}

// listedSponsor is one id in a sponsor list of an entry.
type listedSponsor struct {
	entry labEntry
	tier  string // the tier of the list the id stands in
	id    string
}

// listedSponsors flattens the sponsor lists of the entries into one element per id, in file order.
func listedSponsors(entries []labEntry) []listedSponsor {
	var out []listedSponsor
	for _, e := range entries {
		for _, l := range e.Sponsors.lists() {
			for _, id := range l.ids {
				out = append(out, listedSponsor{entry: e, tier: l.tier, id: id})
			}
		}
	}
	return out
}

// indexBy indexes items by their key.
func indexBy[T any](items []T, key func(T) string) map[string]T {
	index := make(map[string]T, len(items))
	for _, item := range items {
		index[key(item)] = item
	}
	return index
}

// outsideTerm reports whether the sponsorship is known not to cover the month. A term or a month that is not a month
// is not judged here: the file that holds it fails its own check.
func outsideTerm(s sponsor, month string) bool {
	return validTerm(s) && isMonth(month) && !covers(s.Since, s.Until, month)
}

// termText says in a message when a sponsorship runs.
func termText(s sponsor) string {
	if s.Until == nil {
		return "from " + s.Since + " with no end"
	}
	return s.Since + " to " + *s.Until
}

// checkLabLog applies the rules of the design to a lab log and returns one message per problem. s is the sponsors file
// that the entries' sponsor and run ids point at, f holds what the boards show, and thisMonth is the current month
// (YYYY-MM, in UTC). Every rule is a function of its own.
func checkLabLog(doc labLogDoc, s sponsorsDoc, f boardFacts, thisMonth string) []string {
	sponsors := indexBy(s.Sponsors, func(x sponsor) string { return x.ID })
	runs := indexBy(s.Runs, func(x run) string { return x.ID })
	return slices.Concat(
		checkLabLogSchema(doc.Schema),
		checkLabLogMonths(doc.Entries),
		checkLabLogOrder(doc.Entries),
		checkLabLogPublished(doc.Entries),
		checkLabLogCounts(doc.Entries),
		checkLabLogChanges(doc.Entries),
		checkLabLogMachines(doc.Entries, f, thisMonth),
		checkLabLogProjects(doc.Entries),
		checkLabLogReleaseDates(doc.Entries),
		checkLabLogUnknownSponsors(doc.Entries, sponsors),
		checkLabLogSponsorTiers(doc.Entries, sponsors),
		checkLabLogSponsorTerms(doc.Entries, sponsors),
		checkLabLogUnknownRuns(doc.Entries, runs),
		checkLabLogRunMonths(doc.Entries, runs),
		checkLabLogTotals(doc.Entries),
		checkLabLogNext(doc.Entries),
		checkLabLogText(doc.Entries),
	)
}

// checkLabLogSchema: the file declares the schema the site reads. The site takes any other value as an invalid file
// and then shows the lab log's invitation instead of its entries; a file without the key does not decode.
func checkLabLogSchema(schema int) []string {
	var p findings
	if schema != labLogSchema {
		p.addf("schema is %d, but the site reads only schema %d", schema, labLogSchema)
	}
	return p
}

// checkLabLogMonths: the month of an entry is a month.
func checkLabLogMonths(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		if !isMonth(e.Month) {
			p.addf("%s: month is not a month (YYYY-MM)", e.label())
		}
	}
	return p
}

// checkLabLogOrder: each month has one entry and the entries run from the newest month down. Only entries whose month
// is a month are judged, each against the last of those before it.
func checkLabLogOrder(entries []labEntry) []string {
	var p findings
	seen := map[string]bool{}
	previous := ""
	for _, e := range entries {
		if !isMonth(e.Month) {
			continue
		}
		if seen[e.Month] {
			p.addf("duplicate month %q", e.Month)
		}
		seen[e.Month] = true
		if previous != "" && e.Month > previous {
			p.addf("%s: not newest first (it comes after %s, and the entries run from the newest month down)",
				e.label(), previous)
		}
		previous = e.Month
	}
	return p
}

// checkLabLogPublished: the day an entry was published is a date.
func checkLabLogPublished(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		if !isDate(e.Published) {
			p.addf("%s: published is not a date (YYYY-MM-DD): %q", e.label(), e.Published)
		}
	}
	return p
}

// checkLabLogCounts: the hours, the boards and tasks run, and the count of Supporters are not below zero.
func checkLabLogCounts(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		for _, c := range []struct {
			field string
			n     int
		}{
			{"hours", e.Hours},
			{"board_runs", e.BoardRuns},
			{"task_runs", e.TaskRuns},
			{"sponsors.supporters", e.Sponsors.Supporters},
		} {
			if c.n < 0 {
				p.addf("%s: %s is negative (%d)", e.label(), c.field, c.n)
			}
		}
	}
	return p
}

// checkLabLogChanges: a model was added to the boards or measured again.
func checkLabLogChanges(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		for _, m := range e.Models {
			if !slices.Contains(labChanges, m.Change) {
				p.addf("%s: change must be %s, not %q", e.modelLabel(m), strings.Join(labChanges, " or "), m.Change)
			}
		}
	}
	return p
}

// checkLabLogMachines: a machine of a model change is a board's hardware label, written exactly as the board has it, in
// the entries of thisMonth and the month before (and in any of a later month). An older entry keeps the machines it
// named: the boards change, and a refresh that retires a machine must not fail the record of a month long published.
func checkLabLogMachines(entries []labEntry, f boardFacts, thisMonth string) []string {
	var p findings
	for _, e := range entries {
		if beforeLastMonth(e.Month, thisMonth) {
			continue
		}
		for _, m := range e.Models {
			for _, machine := range m.Machines {
				if !f.Machines[machine] {
					p.addf("%s: machine not on the boards: %q", e.modelLabel(m), machine)
				}
			}
		}
	}
	return p
}

// beforeLastMonth reports whether month is a month older than the month before thisMonth. Like ended for a sponsor,
// it lets go only what is known to be past, so that the date alone never fails a file that passed: a month that is not
// a month is left to checkLabLogMonths and stays checked, and so does a month after thisMonth.
func beforeLastMonth(month, thisMonth string) bool {
	return isMonth(month) && month < monthBefore(thisMonth)
}

// checkLabLogProjects: a release is one of taraengine or taracode.
func checkLabLogProjects(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		for _, r := range e.Releases {
			if !slices.Contains(labProjects, r.Project) {
				p.addf("%s: project must be %s", e.releaseLabel(r), strings.Join(labProjects, " or "))
			}
		}
	}
	return p
}

// checkLabLogReleaseDates: the day of a release is a date.
func checkLabLogReleaseDates(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		for _, r := range e.Releases {
			if !isDate(r.Date) {
				p.addf("%s: release date %q is not a date (YYYY-MM-DD)", e.releaseLabel(r), r.Date)
			}
		}
	}
	return p
}

// checkLabLogUnknownSponsors: an id in a sponsor list of an entry is a sponsor of sponsors.json.
func checkLabLogUnknownSponsors(entries []labEntry, sponsors map[string]sponsor) []string {
	var p findings
	for _, l := range listedSponsors(entries) {
		if _, ok := sponsors[l.id]; !ok {
			p.addf("%s: sponsors.%s: unknown sponsor %q (it is not in sponsors.json)", l.entry.label(), l.tier, l.id)
		}
	}
	return p
}

// checkLabLogSponsorTiers: a sponsor is listed under its own tier. The message names the tier of the list and the month
// of the entry.
func checkLabLogSponsorTiers(entries []labEntry, sponsors map[string]sponsor) []string {
	var p findings
	for _, l := range listedSponsors(entries) {
		if s, ok := sponsors[l.id]; ok && s.Tier != l.tier {
			p.addf("%s: sponsors.%s: %q is not %s in %s (its tier is %s)",
				l.entry.label(), l.tier, l.id, l.tier, l.entry.Month, s.Tier)
		}
	}
	return p
}

// checkLabLogSponsorTerms: a sponsor listed under its own tier is current in the month of the entry. A sponsor under
// the wrong tier is reported by the rule above and not twice.
func checkLabLogSponsorTerms(entries []labEntry, sponsors map[string]sponsor) []string {
	var p findings
	for _, l := range listedSponsors(entries) {
		if s, ok := sponsors[l.id]; ok && s.Tier == l.tier && outsideTerm(s, l.entry.Month) {
			p.addf("%s: sponsors.%s: %q is not current in %s (its sponsorship runs %s)",
				l.entry.label(), l.tier, l.id, l.entry.Month, termText(s))
		}
	}
	return p
}

// checkLabLogUnknownRuns: an id in the runs of an entry is a paid run of sponsors.json.
func checkLabLogUnknownRuns(entries []labEntry, runs map[string]run) []string {
	var p findings
	for _, e := range entries {
		for _, id := range e.Runs {
			if _, ok := runs[id]; !ok {
				p.addf("%s: runs: unknown run %q (it is not in sponsors.json)", e.label(), id)
			}
		}
	}
	return p
}

// checkLabLogRunMonths: an entry lists the runs bought in its own month.
func checkLabLogRunMonths(entries []labEntry, runs map[string]run) []string {
	var p findings
	for _, e := range entries {
		for _, id := range e.Runs {
			if r, ok := runs[id]; ok && r.Month != e.Month {
				p.addf("%s: runs: run bought in another month: %q was bought in %s, not %s",
					e.label(), id, r.Month, e.Month)
			}
		}
	}
	return p
}

// checkLabLogTotals: the month's GitHub Sponsors total is not below zero.
func checkLabLogTotals(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		if e.GitHubTotalUSD < 0 {
			p.addf("%s: negative total (github_total_usd is %v)", e.label(), e.GitHubTotalUSD)
		}
	}
	return p
}

// checkLabLogNext: an entry says what is next, in words.
func checkLabLogNext(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		if strings.TrimSpace(e.Next) == "" {
			p.addf("%s: empty next (say in a sentence or two what the next month holds)", e.label())
		}
	}
	return p
}

// checkLabLogText: plain hyphens and straight quotes in every text of every entry.
func checkLabLogText(entries []labEntry) []string {
	var p findings
	for _, e := range entries {
		p.plain(e.label(), stringsOf(e))
	}
	return p
}
