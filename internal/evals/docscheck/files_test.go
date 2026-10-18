package docscheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// docsDir is the folder of the committed files these tests guard, relative to this package.
const docsDir = "../../../docs/evals/"

// scoreboardPath is the board the marks and the machines are checked against.
const scoreboardPath = docsDir + "scoreboard.json"

// fancyRunes are the dashes and quotes the house rule keeps out of every text: U+2013 and U+2014 (the en and em dash)
// and U+2018, U+2019, U+201C and U+201D (the curly single and double quotes). They are written as code points, so that
// no fancy character appears in this source.
var fancyRunes = string([]rune{0x2013, 0x2014, 0x2018, 0x2019, 0x201c, 0x201d})

// utf8BOM is the byte order mark that a UTF-8 file may open with. A JSON file may not; an SVG file may.
var utf8BOM = string([]byte{0xef, 0xbb, 0xbf})

// monthPattern is a month written YYYY-MM, with a month from 01 to 12.
var monthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// decodeStrict reads one JSON document into v, a pointer to one of the file types, and holds the file to that type the
// way the site reads it. encoding/json alone matches a key in any letter case, reads a missing key or a null as an
// empty value, and turns bytes that are not UTF-8 and lone surrogate escapes into U+FFFD; PHP reads keys exactly and
// refuses the whole file for either fault, and the site takes a missing or null list as an invalid file. So the bytes
// are UTF-8, the file is one document with nothing after it, shapeProblems holds every key and every kind of value to
// the type, and the typed decode then refuses what is left: a count with a fraction or out of range.
func decodeStrict(data []byte, v any) error {
	if !utf8.Valid(data) {
		return errors.New("not UTF-8 (save the file as UTF-8)")
	}
	var raw any
	if err := decodeOne(data, &raw); err != nil {
		return err
	}
	if problems := shapeProblems("", raw, reflect.TypeOf(v).Elem()); len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return decodeOne(data, v)
}

// decodeOne reads the one JSON document of data into v. It refuses a second document and trailing text.
func decodeOne(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after the JSON document")
	}
	return nil
}

// shapeProblems walks a JSON value, as decoded into any, beside the Go type t it is read into, and returns one message
// per place where the two part: a key of the type that is missing, a key it does not name, a value of another kind (a
// list given as an object, or a null where the type holds no pointer) and a text that holds U+FFFD. at is the path of
// the value in the file, "" at the top level. A text or a number of the right kind is otherwise left to the typed
// decode.
func shapeProblems(at string, v any, t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		if v == nil {
			return nil
		}
		t = t.Elem()
	}
	if got, want := jsonKind(v), kindFor(t); got != want {
		return []string{fmt.Sprintf("%s: %s, but the rules want %s", where(at), got, want)}
	}
	switch t.Kind() {
	case reflect.Struct:
		return objectProblems(at, v.(map[string]any), t)
	case reflect.Slice:
		return listProblems(at, v.([]any), t.Elem())
	case reflect.String:
		return textProblems(at, v.(string))
	}
	return nil
}

// objectProblems: every key of the struct type t is in the object, spelled as its json tag spells it, and no other key
// is. A key in another letter case is reported as a misspelling of the key and not also as a missing key; foldMatch is
// asked only for a key that has no exact match, so a match it finds is in another letter case.
func objectProblems(at string, object map[string]any, t reflect.Type) []string {
	var p findings
	names, keys := jsonNames(t), slices.Sorted(maps.Keys(object))
	for i, name := range names {
		value, ok := object[name]
		switch {
		case ok:
			p = append(p, shapeProblems(keyPath(at, name), value, t.Field(i).Type)...)
		case foldMatch(keys, name) == "":
			p.addf("%s: missing key %q", where(at), name)
		}
	}
	for _, key := range keys {
		if !slices.Contains(names, key) {
			p.addf("%s: %s", where(at), unknownKey(key, names))
		}
	}
	return p
}

// listProblems walks each item of a list beside the type of the list's items.
func listProblems(at string, items []any, item reflect.Type) []string {
	var p []string
	for i, v := range items {
		p = append(p, shapeProblems(fmt.Sprintf("%s[%d]", at, i), v, item)...)
	}
	return p
}

// textProblems: a text holds no U+FFFD, the replacement character that a lone surrogate escape (which PHP refuses) and
// a broken character decode to.
func textProblems(at, text string) []string {
	var p findings
	if strings.ContainsRune(text, utf8.RuneError) {
		p.addf("%s: holds U+FFFD, the replacement character (a lone surrogate escape or a broken character)", where(at))
	}
	return p
}

// jsonKind names the kind of a JSON value as decoded into any.
func jsonKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "a text"
	case float64:
		return "a number"
	case []any:
		return "a list"
	case map[string]any:
		return "an object"
	}
	return "true or false" // the one kind left, a JSON boolean
}

// kindFor names the kind of JSON value that a field of the Go type t is read from. A kind the file types do not hold
// panics, so that a new kind of field is noticed and not read loosely.
func kindFor(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "a text"
	case reflect.Int, reflect.Float64:
		return "a number"
	case reflect.Slice:
		return "a list"
	case reflect.Struct:
		return "an object"
	}
	panic("the key walk cannot read a " + t.Kind().String())
}

// jsonNames are the keys the fields of the struct type t are read from, in field order: the names of their json tags.
func jsonNames(t reflect.Type) []string {
	names := make([]string, t.NumField())
	for i := range names {
		names[i], _, _ = strings.Cut(t.Field(i).Tag.Get("json"), ",")
	}
	return names
}

// foldMatch is the entry of list that is s when letter case is ignored, or "" when there is none.
func foldMatch(list []string, s string) string {
	i := slices.IndexFunc(list, func(x string) bool { return strings.EqualFold(x, s) })
	if i < 0 {
		return ""
	}
	return list[i]
}

// unknownKey says what is wrong with a key the type does not name: it misspells a key the type names, or it is a key
// of its own.
func unknownKey(key string, names []string) string {
	if name := foldMatch(names, key); name != "" {
		return fmt.Sprintf("key %q must be spelled %q (keys are case-sensitive)", key, name)
	}
	return fmt.Sprintf("unknown key %q", key)
}

// where names a place in a file for a message: the path of keys and indexes that leads to it, or the top level.
func where(at string) string {
	if at == "" {
		return "top level"
	}
	return at
}

// keyPath is the path of the value under key in the object at the path at.
func keyPath(at, key string) string {
	if at == "" {
		return key
	}
	return at + "." + key
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

// The files as first committed, with empty lists.
const (
	emptySponsors = `{"schema": 1, "sponsors": [], "runs": []}` + "\n"
	emptyLabLog   = `{"schema": 1, "entries": []}` + "\n"
)

// designSponsors is a sponsors file written with the keys of the design's example: a sponsor that runs on, and two paid
// runs, one with a named buyer and one without. The decoding cases edit it, so that each breaks one thing.
const designSponsors = `{
  "schema": 1,
  "sponsors": [
    {"id": "example-co", "name": "Example Co", "tier": "lab-sponsor", "url": "https://example.com", "since": "2026-11",
      "until": null, "marks": {"models": [], "machines": [], "engines": []}}
  ],
  "runs": [
    {"id": "example-run", "model": "example-model:27b", "month": "2026-11", "by": "Example Co",
      "url": "https://example.com"},
    {"id": "quiet-run", "model": "example-model:27b", "month": "2026-12", "by": null, "url": null}
  ]
}`

// problemsOf is what a file meets: the error that decoding data into doc gives, or else the problems that check finds
// in what doc then holds.
func problemsOf(data string, doc any, check func() []string) []string {
	if err := decodeStrict([]byte(data), doc); err != nil {
		return []string{err.Error()}
	}
	return check()
}

// decodeCase is a file a decoding case reads: its base with old replaced by new. want is a fragment of the one problem
// decodeStrict reports; it is empty for a file that decodes.
type decodeCase struct{ name, old, new, want string }

// checkDecodeCases decodes each case into a fresh value of newDoc. An old that does not occur in the base would leave
// the base as it is, so that case fails rather than pass for the wrong reason.
func checkDecodeCases(t *testing.T, base string, newDoc func() any, cases []decodeCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(base, c.old) {
				t.Fatalf("the edit %q is not in the file it edits", c.old)
			}
			err := decodeStrict([]byte(strings.Replace(base, c.old, c.new, 1)), newDoc())
			switch {
			case c.want == "" && err != nil:
				t.Errorf("want no error, got %v", err)
			case c.want != "" && (err == nil || strings.Contains(err.Error(), "\n") || !strings.Contains(err.Error(), c.want)):
				t.Errorf("want exactly one problem containing %q, got %v", c.want, err)
			}
		})
	}
}

// TestDecodeStrictHoldsTheFileToItsType: a file is read the way the site reads it, so that CI never passes a file the
// site refuses or reads differently. Every key of the type is present and spelled exactly (keys are case-sensitive),
// no other key stands beside them (so a file cannot carry an amount the design leaves out), a list is a list and an
// object an object, null stands only where the type holds a pointer (until, by and url), the bytes are UTF-8 and no
// text holds U+FFFD (the character a lone surrogate escape decodes to), and nothing follows the one document.
func TestDecodeStrictHoldsTheFileToItsType(t *testing.T) {
	backslash := string(rune(0x5c))
	sponsorsDocs := func() any { return &sponsorsDoc{} }
	checkDecodeCases(t, designSponsors, sponsorsDocs, []decodeCase{
		{"the design's file", "", "", ""},
		{"a surrogate pair, which is one character", "Example Co",
			"Example " + backslash + "ud83d" + backslash + "ude00 Co", ""},
		{"a key in another case", `"schema"`, `"Schema"`, `top level: key "Schema" must be spelled "schema"`},
		{"a key left out", `"until": null, `, "", `sponsors[0]: missing key "until"`},
		{"the key of a null left out", `"by": null, `, "", `runs[1]: missing key "by"`},
		{"a key the design does not name", `"tier": "lab-sponsor",`, `"tier": "lab-sponsor", "amount": 30,`,
			`sponsors[0]: unknown key "amount"`},
		{"a null list", `"machines": []`, `"machines": null`, "sponsors[0].marks.machines: null, but the rules want a list"},
		{"a null object", `"marks": {"models": [], "machines": [], "engines": []}`, `"marks": null`,
			"sponsors[0].marks: null, but the rules want an object"},
		{"a null text", `"name": "Example Co"`, `"name": null`, "sponsors[0].name: null, but the rules want a text"},
		{"a null number", `"schema": 1`, `"schema": null`, "schema: null, but the rules want a number"},
		{"a list given as an object", `"machines": []`, `"machines": {}`,
			"sponsors[0].marks.machines: an object, but the rules want a list"},
		{"an object given as a list", `"marks": {"models": [], "machines": [], "engines": []}`, `"marks": []`,
			"sponsors[0].marks: a list, but the rules want an object"},
		{"a number given as a text", `"schema": 1`, `"schema": "1"`, "schema: a text, but the rules want a number"},
		{"true given for a month", `"until": null`, `"until": true`, "sponsors[0].until: true or false, but the rules"},
		{"a list for the whole file", designSponsors, "[]", "top level: a list, but the rules want an object"},
		{"bytes that are not UTF-8", "Example Co", "Example " + string([]byte{0xff}) + " Co", "not UTF-8"},
		{"a byte order mark", designSponsors, utf8BOM + designSponsors, "invalid character"},
		{"a lone surrogate escape", "Example Co", "Example " + backslash + "ud800 Co", "sponsors[0].name: holds U+FFFD"},
		{"the replacement character itself", "Example Co", "Example " + string(utf8.RuneError) + " Co",
			"sponsors[0].name: holds U+FFFD"},
		{"a second document", designSponsors, designSponsors + designSponsors, "data after the JSON document"},
		{"text after the document", designSponsors, designSponsors + " x", "data after the JSON document"},
		{"not JSON at all", designSponsors, "sponsors", "invalid character"},
	})
	checkDecodeCases(t, emptySponsors, sponsorsDocs, []decodeCase{
		{"the committed sponsors file", "", "", ""},
		{"a null list of sponsors", `"sponsors": []`, `"sponsors": null`, "sponsors: null, but the rules want a list"},
		{"runs given as an object", `"runs": []`, `"runs": {}`, "runs: an object, but the rules want a list"},
	})
	labLogs := func() any { return &labLogDoc{} }
	checkDecodeCases(t, emptyLabLog, labLogs, []decodeCase{
		{"the committed lab log", "", "", ""},
		{"a null list of entries", `"entries": []`, `"entries": null`, "entries: null, but the rules want a list"},
	})
	checkDecodeCases(t, designLog, labLogs, []decodeCase{
		{"the design's lab log", "", "", ""},
		{"a key in another case in an entry", `"lab-sponsor":`, `"Lab-Sponsor":`,
			`entries[0].sponsors: key "Lab-Sponsor" must be spelled "lab-sponsor"`},
		{"a key left out of an entry", `"runs": ["run-one"],`, "", `entries[0]: missing key "runs"`},
		{"a null list in an entry", `"runs": ["run-one"]`, `"runs": null`,
			"entries[0].runs: null, but the rules want a list"},
		{"a null count in an entry", `"hours": 32`, `"hours": null`, "entries[0].hours: null, but the rules want a number"},
	})
}

// TestTheKeyWalkRefusesAKindItCannotRead: the walk knows the kinds of field the file types hold; a map, say, makes it
// panic, so that a new kind of field is noticed and not read loosely.
func TestTheKeyWalkRefusesAKindItCannotRead(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("the key walk on a map must panic")
		}
	}()
	shapeProblems("", map[string]any{}, reflect.TypeFor[map[string]string]())
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
