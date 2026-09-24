package evals

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Fixture is one recorded tool result: the call's signature, the file holding the redacted text,
// and whether the tool returned it as an error.
type Fixture struct {
	Signature string `yaml:"signature"`
	File      string `yaml:"file"`
	Error     bool   `yaml:"error,omitempty"`
}

// Index is fixtures/index.yaml, the authoritative list.
type Index struct {
	Fixtures []Fixture `yaml:"fixtures"`
}

// Store is a task's fixtures on disk, keyed by signature.
type Store struct {
	dir   string
	index Index
	byKey map[string]Fixture

	mu         sync.Mutex
	lookupErrs map[string]error // set by Lookup, keyed by signature, when an indexed file cannot be read
}

// LoadFixtures reads <taskDir>/fixtures/index.yaml; a missing index is an empty store. Every error
// this returns names index.yaml. An entry whose file is not a plain base name, or whose signature
// repeats an earlier entry, makes the whole index invalid (ruling P3-R27): a corrupted or
// hand-edited index must fail loudly rather than serve, or delete, the wrong file.
func LoadFixtures(taskDir string) (*Store, error) {
	s := &Store{dir: filepath.Join(taskDir, "fixtures"), byKey: map[string]Fixture{}, lookupErrs: map[string]error{}}
	indexPath := filepath.Join(s.dir, "index.yaml")
	data, err := os.ReadFile(indexPath) //nolint:gosec // indexPath is built from the trusted corpus path taskDir
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", indexPath, err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s.index); err != nil {
		return nil, fmt.Errorf("%s: %w", indexPath, err)
	}
	for _, f := range s.index.Fixtures {
		if err := validFixtureFileName(f.File); err != nil {
			return nil, fmt.Errorf("%s: fixture %q: %w", indexPath, f.Signature, err)
		}
		if _, dup := s.byKey[f.Signature]; dup {
			return nil, fmt.Errorf("%s: signature %q is indexed twice", indexPath, f.Signature)
		}
		s.byKey[f.Signature] = f
	}
	return s, nil
}

// validFixtureFileName rejects anything but a plain base name: empty, ".", "..", or a name that
// changes when cleaned through filepath.Base (an embedded separator or a path), so an index entry
// can never point outside the fixtures directory (ruling P3-R27). Authored fixtures keep any base
// name they like; it need not match fixtureFileName's own convention.
func validFixtureFileName(name string) error {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return fmt.Errorf("file %q is not a plain file name", name)
	}
	return nil
}

// Len is the number of fixtures.
func (s *Store) Len() int { return len(s.index.Fixtures) }

// Fixtures lists the index entries in order.
func (s *Store) Fixtures() []Fixture { return append([]Fixture(nil), s.index.Fixtures...) }

// Dir is the fixtures directory.
func (s *Store) Dir() string { return s.dir }

// Lookup returns the recorded text for a signature and whether the tool returned it as an error.
// ok is false both for a signature Lookup never found (a miss) and for one whose file is indexed
// but could not be read (a corpus defect); LookupErr(sig) distinguishes the two.
func (s *Store) Lookup(sig string) (output string, isErr bool, ok bool) {
	f, ok := s.byKey[sig]
	if !ok {
		return "", false, false
	}
	data, err := os.ReadFile(filepath.Join(s.dir, f.File))
	if err != nil {
		s.mu.Lock()
		s.lookupErrs[sig] = fmt.Errorf("fixture file %s for signature %q: %w", f.File, sig, err)
		s.mu.Unlock()
		return "", false, false
	}
	return string(data), f.Error, true
}

// LookupErr is the error from Lookup(sig) when its file is indexed but could not be read: a corpus
// defect the runner should surface distinctly from an ordinary miss (a signature Lookup never
// found), which leaves this nil.
func (s *Store) LookupErr(sig string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookupErrs[sig]
}

// Save writes the text under the signature and updates the index; an existing signature is
// replaced and its old file removed when the name changed. It refuses to reuse a file name already
// claimed by a different signature rather than overwrite it (ruling P3-R27).
func (s *Store) Save(sig, output string, isErr bool) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil { //nolint:gosec // the fixtures directory is repository content
		return err
	}
	name := fixtureFileName(sig)
	if owner, used := s.fileOwner(name); used && owner != sig {
		return fmt.Errorf("fixture file %s is already used by signature %q", name, owner)
	}
	f := Fixture{Signature: sig, File: name, Error: isErr}
	dest := filepath.Join(s.dir, f.File)
	if err := os.WriteFile(dest, []byte(output), 0o644); err != nil { //nolint:gosec // fixtures are repository content
		return err
	}
	if old, ok := s.byKey[sig]; ok {
		if old.File != f.File {
			_ = os.Remove(filepath.Join(s.dir, old.File))
		}
		for i := range s.index.Fixtures {
			if s.index.Fixtures[i].Signature == sig {
				s.index.Fixtures[i] = f
			}
		}
	} else {
		s.index.Fixtures = append(s.index.Fixtures, f)
	}
	s.byKey[sig] = f
	data, err := yaml.Marshal(s.index)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, "index.yaml"), data, 0o644) //nolint:gosec // repository content
}

// fileOwner returns the signature that already uses file name, if any.
func (s *Store) fileOwner(name string) (string, bool) {
	for _, f := range s.index.Fixtures {
		if f.File == name {
			return f.Signature, true
		}
	}
	return "", false
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// fixtureFileName is a readable slug of the signature plus six hex characters of its SHA-256.
func fixtureFileName(sig string) string {
	slug := strings.Trim(strings.ToLower(nonAlnum.ReplaceAllString(sig, "-")), "-")
	if len(slug) > 80 {
		slug = strings.TrimRight(slug[:80], "-")
	}
	sum := sha256.Sum256([]byte(sig))
	return slug + "-" + hex.EncodeToString(sum[:3]) + ".txt"
}
