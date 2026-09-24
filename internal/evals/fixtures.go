package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
}

// LoadFixtures reads <taskDir>/fixtures/index.yaml; a missing index is an empty store.
func LoadFixtures(taskDir string) (*Store, error) {
	s := &Store{dir: filepath.Join(taskDir, "fixtures"), byKey: map[string]Fixture{}}
	data, err := os.ReadFile(filepath.Join(s.dir, "index.yaml"))
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &s.index); err != nil {
		return nil, fmt.Errorf("%s: %w", s.dir, err)
	}
	for _, f := range s.index.Fixtures {
		s.byKey[f.Signature] = f
	}
	return s, nil
}

// Len is the number of fixtures.
func (s *Store) Len() int { return len(s.index.Fixtures) }

// Fixtures lists the index entries in order.
func (s *Store) Fixtures() []Fixture { return append([]Fixture(nil), s.index.Fixtures...) }

// Dir is the fixtures directory.
func (s *Store) Dir() string { return s.dir }

// Lookup returns the recorded text for a signature and whether the tool returned it as an error.
func (s *Store) Lookup(sig string) (output string, isErr bool, ok bool) {
	f, ok := s.byKey[sig]
	if !ok {
		return "", false, false
	}
	data, err := os.ReadFile(filepath.Join(s.dir, f.File))
	if err != nil {
		return "", false, false
	}
	return string(data), f.Error, true
}

// Save writes the text under the signature and updates the index; an existing signature is
// replaced and its old file removed when the name changed.
func (s *Store) Save(sig, output string, isErr bool) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil { //nolint:gosec // the fixtures directory is repository content
		return err
	}
	f := Fixture{Signature: sig, File: fixtureFileName(sig), Error: isErr}
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
