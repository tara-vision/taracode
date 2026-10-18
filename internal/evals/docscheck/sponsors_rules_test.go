package docscheck

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
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

// sponsorsSchema is the one version of sponsors.json that the rules and the site read.
const sponsorsSchema = 1

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
// the boards show, logos the ids that have a file docs/evals/sponsors/<id>.svg, and thisMonth the current month
// (YYYY-MM, in UTC). Every rule is a function of its own.
func checkSponsors(doc sponsorsDoc, f boardFacts, logos map[string]bool, thisMonth string) []string {
	return slices.Concat(
		checkSponsorSchema(doc.Schema),
		checkSponsorIDs(doc.Sponsors),
		checkSponsorTiers(doc.Sponsors),
		checkSponsorNames(doc.Sponsors),
		checkSponsorURLs(doc.Sponsors),
		checkSponsorMonths(doc.Sponsors),
		checkTierCaps(doc.Sponsors),
		checkMarkTiers(doc.Sponsors),
		checkMarksOnBoard(doc.Sponsors, f, thisMonth),
		checkLogoPresence(doc.Sponsors, logos),
		checkPaidRuns(doc.Runs),
		checkRunModels(doc.Runs),
		checkRunBuyers(doc.Runs),
		checkSponsorsText(doc),
	)
}

// checkSponsorSchema: the file declares the schema the site reads. The site takes any other value as an invalid file
// and then shows no mark, no credit and no Title line; a file without the key does not decode.
func checkSponsorSchema(schema int) []string {
	var p findings
	if schema != sponsorsSchema {
		p.addf("schema is %d, but the site reads only schema %d", schema, sponsorsSchema)
	}
	return p
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

// checkSponsorNames: a credit shows the sponsor's name, so the name is not empty.
func checkSponsorNames(sponsors []sponsor) []string {
	var p findings
	for _, s := range sponsors {
		if strings.TrimSpace(s.Name) == "" {
			p.addf("%s: empty name (a credit shows the sponsor's name)", s.label())
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

// checkMarkTiers: a Title sponsor has no marks and a Supporter has some, whether the sponsorship runs or has ended.
func checkMarkTiers(sponsors []sponsor) []string {
	var p findings
	for _, s := range sponsors {
		switch {
		case s.Tier == "title" && !s.Marks.empty():
			p.addf("%s: title with marks (a company on the boards cannot hold the Title)", s.label())
		case s.Tier == "supporter" && s.Marks.empty():
			p.addf("%s: supporter without marks (Supporter is for a company on the boards)", s.label())
		}
	}
	return p
}

// ended reports whether the sponsorship's last month is before thisMonth. A last month that is not a month is left to
// checkSponsorMonths, and the sponsorship counts as running.
func ended(s sponsor, thisMonth string) bool {
	return s.Until != nil && isMonth(*s.Until) && *s.Until < thisMonth
}

// checkMarksOnBoard: every mark of a sponsorship that runs in thisMonth or later points at something on the boards, so
// that a typo cannot drop a disclosure. A sponsorship that has ended keeps the marks it had: the boards change, and a
// refresh that retires a model or a machine must not fail its record or force it to be erased.
func checkMarksOnBoard(sponsors []sponsor, f boardFacts, thisMonth string) []string {
	var p findings
	for _, s := range sponsors {
		if ended(s, thisMonth) {
			continue
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

// checkRunModels: a run names the model that was bought, so the model is not empty. It is not matched against the
// boards: a model may be bought before its rows exist.
func checkRunModels(runs []run) []string {
	var p findings
	for _, r := range runs {
		if strings.TrimSpace(r.Model) == "" {
			p.addf("%s: empty run model (name the model exactly as the boards do)", r.label())
		}
	}
	return p
}

// checkRunBuyers: by is null for a buyer who stays unnamed and the buyer's name otherwise, never an empty string.
func checkRunBuyers(runs []run) []string {
	var p findings
	for _, r := range runs {
		if r.By != nil && strings.TrimSpace(*r.By) == "" {
			p.addf("%s: empty buyer (by is set but empty; use null for an unnamed buyer)", r.label())
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
