package evals

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Corpus names the corpus a run measured: its version (the seal in evals/VERSION) and the fingerprint of every
// task file, workdir, policy and fixture, the common store included (spec 1f). A board holds rows of one version
// only; a results file without the block ran corpus 1, the corpus before the seal.
type Corpus struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// sealFile is the seal beside the tasks root: evals/VERSION for evals/tasks.
const sealFile = "VERSION"

// evalsDir is the directory the tasks root lies in, which holds the seal and the common store: the absolute parent
// of the tasks root, so a trailing slash, "." or a relative spelling all name the same place.
func evalsDir(tasksRoot string) string {
	if abs, err := filepath.Abs(tasksRoot); err == nil {
		tasksRoot = abs
	}
	return filepath.Dir(filepath.Clean(tasksRoot))
}

// CommonDir is the common fixture store beside a tasks root (evals/common for evals/tasks), "" when it has no
// fixtures index yet.
func CommonDir(tasksRoot string) string {
	dir := filepath.Join(evalsDir(tasksRoot), "common")
	if _, err := os.Stat(filepath.Join(dir, "fixtures", "index.yaml")); err != nil {
		return ""
	}
	return dir
}

// hashed reports whether a file counts for the corpus hash: the files of the corpus and never what the machine or a
// failed recording leaves beside them (.DS_Store, ._* resource forks, a fixtures-recording-* or fixtures.replaced
// directory).
func hashed(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		if part == ".DS_Store" || strings.HasPrefix(part, "._") || strings.HasPrefix(part, "fixtures-recording-") ||
			part == "fixtures.replaced" {
			return false
		}
	}
	return true
}

// CorpusHash is the sha256 over every regular file under the tasks root and under the common store beside it,
// in the order of their slash-separated paths relative to the evals directory: each path, a NUL, the bytes, a
// NUL. The same files in another directory hash the same; any changed, added or removed file changes it.
func CorpusHash(tasksRoot string) (string, error) {
	base := evalsDir(tasksRoot)
	var paths []string
	tasks := filepath.Join(base, filepath.Base(filepath.Clean(tasksRoot)))
	for _, root := range []string{tasks, filepath.Join(base, "common")} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && p == root {
					return filepath.SkipDir
				}
				return err
			}
			if d.Type().IsRegular() && hashed(relSlash(base, p)) {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("corpus hash: %w", err)
		}
	}
	sort.Slice(paths, func(i, j int) bool { return relSlash(base, paths[i]) < relSlash(base, paths[j]) })
	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // p walks the corpus, repository content
		if err != nil {
			return "", fmt.Errorf("corpus hash: %w", err)
		}
		h.Write([]byte(relSlash(base, p)))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// relSlash is p's slash-separated path under base, where the walks above found it.
func relSlash(base, p string) string {
	return filepath.ToSlash(strings.TrimPrefix(p, base+string(filepath.Separator)))
}

// ReadSeal reads evals/VERSION: the version and the hash the corpus was sealed with. A corpus without a seal is
// version 1 with no hash. A seal that is not a whole number and a sha256 is refused with the file's name.
func ReadSeal(tasksRoot string) (Corpus, error) {
	path := filepath.Join(evalsDir(tasksRoot), sealFile)
	f, err := os.Open(path) //nolint:gosec // the seal is repository content beside the corpus
	if errors.Is(err, os.ErrNotExist) {
		return Corpus{Version: "1"}, nil
	}
	if err != nil {
		return Corpus{}, fmt.Errorf("%s: %w", sealFile, err)
	}
	defer func() { _ = f.Close() }()
	var c Corpus
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "version:"):
			c.Version = strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		case strings.HasPrefix(line, "hash:"):
			c.Hash = strings.TrimSpace(strings.TrimPrefix(line, "hash:"))
		default:
			return Corpus{}, fmt.Errorf("%s: a line that is neither version nor hash: %q", sealFile, line)
		}
	}
	if err := sc.Err(); err != nil {
		return Corpus{}, fmt.Errorf("%s: %w", sealFile, err)
	}
	if _, err := strconv.Atoi(c.Version); err != nil || c.Version == "" {
		return Corpus{}, fmt.Errorf("%s: the version must be a whole number, not %q", sealFile, c.Version)
	}
	if c.Hash != "" && !isHash(c.Hash) {
		return Corpus{}, fmt.Errorf("%s: the hash must be a sha256 in hex, not %q", sealFile, c.Hash)
	}
	return c, nil
}

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Seal writes evals/VERSION with the version and the corpus's current hash. The version is a whole number greater
// than the sealed one: a corpus that changed is a new version, and a board never mixes two.
func Seal(tasksRoot, version string) (Corpus, error) {
	n, err := strconv.Atoi(version)
	if err != nil || n < 1 {
		return Corpus{}, fmt.Errorf("seal: the version must be a whole number, not %q", version)
	}
	current, err := ReadSeal(tasksRoot)
	if err != nil {
		return Corpus{}, err
	}
	if sealed, _ := strconv.Atoi(current.Version); current.Hash != "" && n <= sealed {
		return Corpus{}, fmt.Errorf("seal: the corpus is sealed as version %s; a resealed corpus takes a greater version",
			current.Version)
	}
	hash, err := CorpusHash(tasksRoot)
	if err != nil {
		return Corpus{}, err
	}
	c := Corpus{Version: strconv.Itoa(n), Hash: hash}
	text := fmt.Sprintf("version: %s\nhash: %s\n", c.Version, c.Hash)
	path := filepath.Join(evalsDir(tasksRoot), sealFile)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil { //nolint:gosec // repository content
		return Corpus{}, fmt.Errorf("seal: %w", err)
	}
	return c, nil
}

// lintSeal is the one lint of the seal: a sealed corpus whose hash is not the corpus's any more changed under its
// seal. A corpus without a seal is not checked.
func lintSeal(tasksRoot string) []string {
	c, err := ReadSeal(tasksRoot)
	if err != nil {
		return []string{err.Error()}
	}
	if c.Hash == "" {
		return nil
	}
	hash, err := CorpusHash(tasksRoot)
	if err != nil {
		return []string{err.Error()}
	}
	if hash != c.Hash {
		return []string{fmt.Sprintf("%s: the corpus changed since it was sealed as version %s; check git status for a file "+
			"left beside the corpus, then bump the version and run taracode eval lint --seal --version <n>",
			filepath.Join(filepath.Base(evalsDir(tasksRoot)), sealFile), c.Version)}
	}
	return nil
}

// CorpusVersion is the version of the corpus a result ran: "1" for a results file without a corpus block.
func (r Results) CorpusVersion() string {
	if r.Corpus == nil || r.Corpus.Version == "" {
		return "1"
	}
	return r.Corpus.Version
}

// CheckCorpusVersions refuses results of two corpus versions, naming every file and its version: rows of two
// versions never share a board (spec 1f).
func CheckCorpusVersions(all []Results) error {
	versions := map[string]bool{}
	var lines []string
	for _, r := range all {
		versions[r.CorpusVersion()] = true
		lines = append(lines, fmt.Sprintf("%s.json: corpus version %s", ResultsName(r), r.CorpusVersion()))
	}
	if len(versions) <= 1 {
		return nil
	}
	sort.Strings(lines)
	return fmt.Errorf("the results are of %d corpus versions and a board holds one; remove the older files or re-run "+
		"them on the current corpus:\n  %s", len(versions), strings.Join(lines, "\n  "))
}
