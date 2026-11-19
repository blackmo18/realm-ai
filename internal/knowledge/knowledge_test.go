package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRememberListGetForget(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "knowledge"))
	if idx, err := s.Index(); err != nil || idx != "" {
		t.Fatalf("empty index: %q %v", idx, err)
	}
	e, err := s.Remember("Deploy target", "We deploy to fly.io from main.")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(e.ID, "deploy-target-") {
		t.Fatalf("id = %q", e.ID)
	}
	got, err := s.Get(e.ID)
	if err != nil || got.Title != "Deploy target" || got.Body != "We deploy to fly.io from main." {
		t.Fatalf("get: %+v %v", got, err)
	}
	idx, _ := s.Index()
	if !strings.Contains(idx, "Deploy target ("+e.ID+")") {
		t.Fatalf("index missing entry: %q", idx)
	}
	if err := s.Forget(e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if idx, _ := s.Index(); idx != "" {
		t.Fatalf("index not cleared: %q", idx)
	}
}

func TestRememberValidation(t *testing.T) {
	s := Open(t.TempDir())
	if _, err := s.Remember(" ", "body"); err == nil {
		t.Fatal("empty title accepted")
	}
	if _, err := s.Remember("t", " "); err == nil {
		t.Fatal("empty body accepted")
	}
	e, err := s.Remember("!!!", "punctuation title")
	if err != nil || !strings.HasPrefix(e.ID, "note-") {
		t.Fatalf("slug fallback: %+v %v", e, err)
	}
	long := strings.Repeat("abc ", 30)
	e, err = s.Remember(long, "x")
	if err != nil || len(e.ID) > 60 {
		t.Fatalf("long title: %q %v", e.ID, err)
	}
}

func TestGetRejectsTraversal(t *testing.T) {
	s := Open(t.TempDir())
	if _, err := s.Get("../secret"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := s.Forget("../secret"); err == nil {
		t.Fatal("path traversal accepted by Forget")
	}
}

func TestRecallRanksByMatches(t *testing.T) {
	s := Open(t.TempDir())
	_, _ = s.Remember("Auth", "JWT tokens expire after 1h. JWT signed with HS256.")
	_, _ = s.Remember("DB", "Postgres 16, JWT secret in vault.")
	_, _ = s.Remember("CI", "GitHub Actions.")
	hits, err := s.Recall("jwt", 0)
	if err != nil || len(hits) != 2 || hits[0].Title != "Auth" {
		t.Fatalf("recall: %+v %v", hits, err)
	}
	hits, _ = s.Recall("jwt", 1)
	if len(hits) != 1 {
		t.Fatalf("limit ignored: %d", len(hits))
	}
	if hits, _ := s.Recall("kubernetes", 5); len(hits) != 0 {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

func TestCopyFromKeepsExistingAndMarksOrigin(t *testing.T) {
	src := Open(filepath.Join(t.TempDir(), "src"))
	dst := Open(filepath.Join(t.TempDir(), "dst"))
	_, _ = src.Remember("Style", "Use gofmt.")
	_, _ = src.Remember("Owner", "Team platform.")
	_, _ = dst.Remember("Own note", "Mine.")
	n, err := dst.CopyFrom(src, "@global/old")
	if err != nil || n != 2 {
		t.Fatalf("copy: %d %v", n, err)
	}
	all, _ := dst.List()
	if len(all) != 3 {
		t.Fatalf("want 3 entries, got %d", len(all))
	}
	origins := 0
	for _, e := range all {
		if e.Origin == "@global/old" {
			origins++
		}
	}
	if origins != 2 {
		t.Fatalf("origin marked on %d entries, want 2", origins)
	}
	empty := Open(filepath.Join(t.TempDir(), "none"))
	if n, err := dst.CopyFrom(empty, "x"); err != nil || n != 0 {
		t.Fatalf("copy from empty: %d %v", n, err)
	}
}

func TestListSkipsForeignFiles(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	_, _ = s.Remember("A", "a")
	_ = os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "Bad Name.md"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "plain.md"), []byte("no frontmatter"), 0o644)
	all, err := s.List()
	if err != nil || len(all) != 2 {
		t.Fatalf("list: %+v %v", all, err)
	}
}
