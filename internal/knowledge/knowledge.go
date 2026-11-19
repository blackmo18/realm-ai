// Package knowledge stores a bot's persistent knowledge as markdown files
// with an INDEX.md that is injected into every turn.
package knowledge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/blackmo18/realm-ai/internal/ids"
)

// Entry is one knowledge note.
type Entry struct {
	ID      string
	Title   string
	Body    string
	Created string
	Origin  string
}

// Store manages a knowledge folder.
type Store struct {
	dir string
}

// ErrNotFound is returned for an unknown entry id.
var ErrNotFound = errors.New("knowledge entry not found")

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	slugRe  = regexp.MustCompile(`[^a-z0-9]+`)
	wordRe  = regexp.MustCompile(`[a-z0-9]+`)
	indexMD = "INDEX.md"
)

// Open returns a store rooted at dir.
func Open(dir string) *Store { return &Store{dir: dir} }

// Dir returns the folder path.
func (s *Store) Dir() string { return s.dir }

// Remember writes a new entry and refreshes the index.
func (s *Store) Remember(title, body string) (Entry, error) {
	return s.write(Entry{Title: title, Body: body, Created: time.Now().UTC().Format(time.RFC3339)})
}

func (s *Store) write(e Entry) (Entry, error) {
	title := strings.TrimSpace(strings.ReplaceAll(e.Title, "\n", " "))
	if title == "" {
		return Entry{}, errors.New("title is required")
	}
	if strings.TrimSpace(e.Body) == "" {
		return Entry{}, errors.New("body is required")
	}
	slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "note"
	}
	out := Entry{ID: slug + "-" + ids.New()[8:], Title: title, Body: strings.TrimSpace(e.Body), Created: e.Created, Origin: e.Origin}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return Entry{}, fmt.Errorf("create knowledge folder: %w", err)
	}
	if err := os.WriteFile(s.path(out.ID), []byte(render(out)), 0o644); err != nil {
		return Entry{}, fmt.Errorf("write knowledge: %w", err)
	}
	return out, s.rebuildIndex()
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".md") }

func render(e Entry) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %s\ncreated: %s\n", e.Title, e.Created)
	if e.Origin != "" {
		fmt.Fprintf(&b, "origin: %s\n", e.Origin)
	}
	b.WriteString("---\n")
	b.WriteString(e.Body)
	b.WriteString("\n")
	return b.String()
}

func parse(id, content string) Entry {
	e := Entry{ID: id}
	rest, ok := strings.CutPrefix(content, "---\n")
	if !ok {
		e.Body = strings.TrimSpace(content)
		return e
	}
	head, body, _ := strings.Cut(rest, "\n---\n")
	for line := range strings.SplitSeq(head, "\n") {
		k, v, _ := strings.Cut(line, ": ")
		switch k {
		case "title":
			e.Title = v
		case "created":
			e.Created = v
		case "origin":
			e.Origin = v
		}
	}
	e.Body = strings.TrimSpace(body)
	return e
}

// Get loads one entry.
func (s *Store) Get(id string) (Entry, error) {
	if !idRe.MatchString(id) {
		return Entry{}, fmt.Errorf("invalid entry id %q", id)
	}
	data, err := os.ReadFile(s.path(id))
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Entry{}, err
	}
	return parse(id, string(data)), nil
}

// List returns all entries, oldest first.
func (s *Store) List() ([]Entry, error) {
	files, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, f := range files {
		name := f.Name()
		if f.IsDir() || name == indexMD || !strings.HasSuffix(name, ".md") {
			continue
		}
		e, err := s.Get(strings.TrimSuffix(name, ".md"))
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created == out[j].Created {
			return out[i].ID < out[j].ID
		}
		return out[i].Created < out[j].Created
	})
	return out, nil
}

// Forget deletes an entry.
func (s *Store) Forget(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	if err := os.Remove(s.path(id)); err != nil {
		return err
	}
	return s.rebuildIndex()
}

// Recall returns entries ranked by how many query words they contain.
func (s *Store) Recall(query string, limit int) ([]Entry, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	terms := wordRe.FindAllString(strings.ToLower(query), -1)
	type scored struct {
		e     Entry
		score int
	}
	var hits []scored
	for _, e := range all {
		text := strings.ToLower(e.Title + " " + e.Body)
		score := 0
		for _, t := range terms {
			score += strings.Count(text, t)
		}
		if score > 0 {
			hits = append(hits, scored{e, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out, nil
}

// Index returns INDEX.md content (empty if no knowledge yet).
func (s *Store) Index() (string, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, indexMD))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

func (s *Store) rebuildIndex() error {
	entries, err := s.List()
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "- %s (%s)\n", e.Title, e.ID)
	}
	return os.WriteFile(filepath.Join(s.dir, indexMD), []byte(b.String()), 0o644)
}

// CopyFrom copies every entry from src into s, marking each with origin.
// Existing entries in s are kept; copies get fresh ids.
func (s *Store) CopyFrom(src *Store, origin string) (int, error) {
	entries, err := src.List()
	if err != nil {
		return 0, fmt.Errorf("read source knowledge: %w", err)
	}
	for _, e := range entries {
		copied := e
		if copied.Origin == "" {
			copied.Origin = origin
		}
		if _, err := s.write(copied); err != nil {
			return 0, fmt.Errorf("copy %s: %w", e.ID, err)
		}
	}
	return len(entries), nil
}
