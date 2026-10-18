package docscheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// docsDir is the folder of the committed files these tests guard, relative to this package.
const docsDir = "../../../docs/evals/"

// scoreboardPath is the board the marks and the machines are checked against.
const scoreboardPath = docsDir + "scoreboard.json"

// fancyRunes are the dashes and quotes the house rule keeps out of every text: U+2013 and U+2014 (the en and em dash)
// and U+2018, U+2019, U+201C and U+201D (the curly single and double quotes). They are written as code points, so that
// no fancy character appears in this source.
var fancyRunes = string([]rune{0x2013, 0x2014, 0x2018, 0x2019, 0x201c, 0x201d})

// monthPattern is a month written YYYY-MM, with a month from 01 to 12.
var monthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// decodeStrict reads one JSON document into v. It refuses a key the type does not name (so a file cannot carry an
// amount or anything else the design leaves out), a second document and trailing text.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the JSON document")
	}
	return nil
}

// readJSON decodes the file at path into v with decodeStrict and fails the test when it cannot.
func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeStrict(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// boardFacts is what the boards show, the things a sponsor's marks and a lab-log entry's machines must point at.
type boardFacts struct {
	Machines map[string]bool // the hardware label of every board
	Models   []string        // every model on a board or in a tier table, once each
	Engines  []string        // every engine a row names, like "taraengine 0.3.1", once each
}

// scoreboardFile names the keys of scoreboard.json the facts read. The file has many more, so it is decoded leniently
// and not with readJSON.
type scoreboardFile struct {
	Boards []struct {
		Hardware  string     `json:"hardware"`
		Rows      []boardRow `json:"rows"`
		DidNotFit []boardRow `json:"did_not_fit"`
		Partial   []boardRow `json:"partial"`
	} `json:"boards"`
	Tiers []struct {
		Rows []boardRow `json:"rows"`
	} `json:"tiers"`
}

// boardRow is the part of a row the facts read: the model and the engine that ran it.
type boardRow struct {
	Model  string `json:"model"`
	Server struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"server"`
	Ollama string `json:"ollama"`
}

// engine is the engine a row names: its server and version, or Ollama with the version a 3.2.x result recorded.
func (r boardRow) engine() string {
	switch {
	case r.Server.Name != "":
		return strings.TrimSpace(r.Server.Name + " " + r.Server.Version)
	case r.Ollama != "":
		return "Ollama " + r.Ollama
	}
	return ""
}

// parseBoardFacts reads the facts out of the bytes of a scoreboard.json: machines from the boards' hardware, models and
// engines from the ranked, did-not-fit and partial rows of every board and from the tier tables. A board that yields no
// machine, model or engine has changed shape, which would leave every rule that points at it with nothing to match.
func parseBoardFacts(data []byte) (boardFacts, error) {
	var file scoreboardFile
	if err := json.Unmarshal(data, &file); err != nil {
		return boardFacts{}, err
	}
	machines := map[string]bool{}
	var rows []boardRow
	for _, b := range file.Boards {
		machines[b.Hardware] = true
		rows = slices.Concat(rows, b.Rows, b.DidNotFit, b.Partial)
	}
	for _, tier := range file.Tiers {
		rows = slices.Concat(rows, tier.Rows)
	}
	var models, engines []string
	for _, r := range rows {
		if r.Model == "" {
			continue
		}
		models = append(models, r.Model)
		if engine := r.engine(); engine != "" {
			engines = append(engines, engine)
		}
	}
	facts := boardFacts{Machines: machines, Models: uniqueSorted(models), Engines: uniqueSorted(engines)}
	if len(facts.Machines) == 0 || len(facts.Models) == 0 || len(facts.Engines) == 0 {
		return boardFacts{}, errors.New("no machine, model or engine: did the file change shape?")
	}
	return facts, nil
}

// uniqueSorted is a sorted copy of list with each entry once.
func uniqueSorted(list []string) []string { return slices.Compact(slices.Sorted(slices.Values(list))) }

// loadBoardFacts reads the facts from the committed scoreboard.json.
func loadBoardFacts(t *testing.T) boardFacts {
	t.Helper()
	data, err := os.ReadFile(scoreboardPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := parseBoardFacts(data)
	if err != nil {
		t.Fatalf("%s: %v", scoreboardPath, err)
	}
	return facts
}

// stringsOf lists every string a value holds, in field order: its string fields, those behind pointers and in slices,
// and those of nested structs. The rule that reads every text of an entry takes them from here, so that a field added
// later is read without anyone listing it. A type it cannot read (a map, say) panics, so that a new kind of field is
// noticed and not skipped.
func stringsOf(v any) []string { return collectStrings(reflect.ValueOf(v)) }

func collectStrings(v reflect.Value) []string {
	var out []string
	switch v.Kind() {
	case reflect.String:
		out = append(out, v.String())
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			out = collectStrings(v.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			out = append(out, collectStrings(v.Index(i))...)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			out = append(out, collectStrings(v.Field(i))...)
		}
	case reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		panic("stringsOf cannot read a " + v.Kind().String())
	}
	return out
}

// plainText reports whether s keeps to plain hyphens and straight quotes.
func plainText(s string) bool { return !strings.ContainsAny(s, fancyRunes) }

// isMonth reports whether s is a month written YYYY-MM.
func isMonth(s string) bool { return monthPattern.MatchString(s) }

// covers reports whether month lies from since to until, both included. A nil until is a sponsorship that runs on.
// The months must be valid: valid YYYY-MM strings compare in calendar order.
func covers(since string, until *string, month string) bool {
	return since <= month && (until == nil || month <= *until)
}

// findings collects the problems one rule finds, one message each.
type findings []string

// addf adds one problem, formatted like fmt.Sprintf.
func (p *findings) addf(format string, args ...any) { *p = append(*p, fmt.Sprintf(format, args...)) }

// plain adds a problem for each of texts that holds a fancy dash or quote; owner names the entry they belong to.
func (p *findings) plain(owner string, texts []string) {
	for _, text := range texts {
		if !plainText(text) {
			p.addf("%s: fancy character in %q (use a plain hyphen or a straight quote)", owner, text)
		}
	}
}

// strPtr is a pointer to a copy of s, for the optional fields of a fixture.
func strPtr(s string) *string { return &s }

// orEmpty is the text p points at, or "" for nil.
func orEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// TestDecodeStrictRefusesWhatTheTypeDoesNotName: a key the type leaves out (an amount on a sponsor, which the design
// has no field for), a second document and trailing text are errors, so a file cannot carry more than the design
// allows.
func TestDecodeStrictRefusesWhatTheTypeDoesNotName(t *testing.T) {
	cases := []struct {
		name, data string
		ok         bool
	}{
		{"a document the type names", `{"schema": 1, "sponsors": [], "runs": []}` + "\n", true},
		{"an amount on a sponsor", `{"schema": 1, "sponsors": [{"id": "a", "amount": 30}], "runs": []}`, false},
		{"an unknown top-level key", `{"schema": 1, "sponsors": [], "runs": [], "extra": 1}`, false},
		{"a second document", `{"schema": 1} {"schema": 2}`, false},
		{"text after the document", `{"schema": 1} x`, false},
		{"a value of the wrong type", `{"schema": "1"}`, false},
		{"not JSON at all", `sponsors`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var doc sponsorsDoc
			if err := decodeStrict([]byte(c.data), &doc); (err == nil) != c.ok {
				t.Errorf("decodeStrict(%s) error = %v, want an error: %v", c.data, err, !c.ok)
			}
		})
	}
}

// TestBoardFactsCollectMachinesModelsAndEngines: machines are the boards' hardware labels; models come from the ranked,
// did-not-fit and partial rows of every board and from the tier tables; an engine is the server a row names or, for a
// 3.2.x row, Ollama with its version. Keys the facts do not need are ignored, since the file carries many more.
func TestBoardFactsCollectMachinesModelsAndEngines(t *testing.T) {
	const board = `{
	  "generated_at": "2026-10-17",
	  "boards": [{
	    "hardware": "Box A (8 GB)",
	    "rows": [{"model": "alpha:1b", "pass_rate": 0.9, "server": {"name": "taraengine", "version": "0.3.1"}}],
	    "did_not_fit": [{"model": "beta:70b", "server": {"name": "taraengine", "version": "0.3.1"}}],
	    "partial": [{"model": "gamma:1b", "server": {"name": "", "version": ""}, "ollama": "0.35.0"}]
	  }],
	  "tiers": [{"tier": "16", "rows": [{"model": "delta:8b", "server": {"name": "", "version": ""}}]}]
	}`
	got, err := parseBoardFacts([]byte(board))
	if err != nil {
		t.Fatal(err)
	}
	want := boardFacts{
		Machines: map[string]bool{"Box A (8 GB)": true},
		Models:   []string{"alpha:1b", "beta:70b", "delta:8b", "gamma:1b"},
		Engines:  []string{"Ollama 0.35.0", "taraengine 0.3.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("facts = %+v\nwant   %+v", got, want)
	}
	for _, c := range []struct{ name, data string }{
		{"not JSON", "{"},
		{"no board", `{"boards": [], "tiers": []}`},
		{"a board without rows", `{"boards": [{"hardware": "A"}]}`},
		{"rows without an engine", `{"boards": [{"hardware": "A", "rows": [{"model": "m"}]}]}`},
		{"rows without a model", `{"boards": [{"hardware": "A", "rows": [{"server": {"name": "taraengine"}}]}]}`},
		{"rows without a machine", `{"tiers": [{"rows": [{"model": "m", "server": {"name": "taraengine"}}]}]}`},
	} {
		if _, err := parseBoardFacts([]byte(c.data)); err == nil {
			t.Errorf("a board file with %s must be an error", c.name)
		}
	}
}

// TestStringsOfReadsEveryStringAValueHolds: strings in fields, behind pointers, in slices and in nested structs all
// count; a nil pointer, a number and a boolean hold none; a map is refused loudly.
func TestStringsOfReadsEveryStringAValueHolds(t *testing.T) {
	s := sponsor{ID: "id", Name: "name", Tier: "tier", URL: "url", Since: "since", Until: strPtr("until"),
		Marks: marks{Models: []string{"m1", "m2"}, Machines: []string{"h"}, Engines: []string{"e"}}}
	want := []string{"id", "name", "tier", "url", "since", "until", "m1", "m2", "h", "e"}
	if got := stringsOf(s); !slices.Equal(got, want) {
		t.Errorf("stringsOf(sponsor) = %q, want %q", got, want)
	}
	r := run{ID: "id", Model: "model", Month: "month"}
	if got, want := stringsOf(r), []string{"id", "model", "month"}; !slices.Equal(got, want) {
		t.Errorf("stringsOf(run) = %q, want %q", got, want)
	}
	if got := stringsOf(sponsorsDoc{Schema: 1}); len(got) != 0 {
		t.Errorf("stringsOf(a document with no entries) = %q, want none", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("stringsOf(a map) must panic")
		}
	}()
	stringsOf(map[string]string{"key": "value"})
}

// TestPlainTextRefusesFancyDashesAndQuotes: the house rule, plain hyphens and straight quotes. Accented and Cyrillic
// letters are fine; each of the six fancy characters is refused wherever it stands in a text.
func TestPlainTextRefusesFancyDashesAndQuotes(t *testing.T) {
	accented, cyrillic := string(rune(0xe9)), string(rune(0x416))
	for _, text := range []string{"Example Co - the lab's partner", `say "hi"`, "", "caf" + accented, cyrillic} {
		if !plainText(text) {
			t.Errorf("plainText(%+q) = false, want true", text)
		}
	}
	for _, r := range []rune{0x2013, 0x2014, 0x2018, 0x2019, 0x201c, 0x201d} {
		fancy := string(r)
		for _, text := range []string{fancy, fancy + " after", "before " + fancy, "in" + fancy + "side"} {
			if plainText(text) {
				t.Errorf("plainText(%+q) = true, want false", text)
			}
		}
	}
}

// TestIsMonthWantsYearDashTwoDigitMonth: YYYY-MM with a month from 01 to 12, nothing before or after it.
func TestIsMonthWantsYearDashTwoDigitMonth(t *testing.T) {
	for _, s := range []string{"2026-01", "2026-09", "2026-10", "2026-12", "1999-10"} {
		if !isMonth(s) {
			t.Errorf("isMonth(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "2026-00", "2026-13", "2026-1", "26-01", "2026/01", "2026-01-01", " 2026-01",
		"2026-01 ", "2026-01\n", "2026-1a", "abcd-01"} {
		if isMonth(s) {
			t.Errorf("isMonth(%q) = true, want false", s)
		}
	}
}

// TestCoversCountsBothEndsAndAnOpenEnd: a sponsorship covers its first and its last month, so a sponsor whose until is
// this month is still current all month; a nil until runs on; one that ends before it starts covers nothing.
func TestCoversCountsBothEndsAndAnOpenEnd(t *testing.T) {
	cases := []struct {
		since string
		until *string
		month string
		want  bool
	}{
		{"2026-11", nil, "2026-10", false},
		{"2026-11", nil, "2026-11", true},
		{"2026-11", nil, "2031-01", true},
		{"2026-11", strPtr("2026-12"), "2026-10", false},
		{"2026-11", strPtr("2026-12"), "2026-11", true},
		{"2026-11", strPtr("2026-12"), "2026-12", true},
		{"2026-11", strPtr("2026-12"), "2027-01", false},
		{"2026-11", strPtr("2026-10"), "2026-10", false},
		{"2026-11", strPtr("2026-10"), "2026-11", false},
	}
	for _, c := range cases {
		if got := covers(c.since, c.until, c.month); got != c.want {
			t.Errorf("covers(%q, %v, %q) = %v, want %v", c.since, orEmpty(c.until), c.month, got, c.want)
		}
	}
}
