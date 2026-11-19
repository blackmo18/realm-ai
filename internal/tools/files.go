package tools

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/blackmo18/realm-ai/internal/runtime"
)

// File tool names (API-key runtimes only; claude-cli has its own tools).
const (
	ReadFile = "read_file"
	Grep     = "grep"
	Glob     = "glob"
)

const (
	maxReadBytes = 256 << 10
	maxMatches   = 200
)

// FileDefs returns read-only file tool definitions.
func FileDefs() []runtime.ToolDef {
	return []runtime.ToolDef{
		{Name: ReadFile, Description: "Read a text file inside the project folder.",
			Schema: obj(map[string]any{"path": str("Path relative to the project folder")}, "path")},
		{Name: Grep, Description: "Search files inside the project folder with a regular expression.",
			Schema: obj(map[string]any{"pattern": str("Go regular expression"), "path": str("Sub-folder to search (optional)")}, "pattern")},
		{Name: Glob, Description: "List files inside the project folder matching a glob, e.g. internal/*.go.",
			Schema: obj(map[string]any{"pattern": str("Glob pattern relative to the project folder")}, "pattern")},
	}
}

// IsFileTool reports whether name is a file tool.
func IsFileTool(name string) bool { return name == ReadFile || name == Grep || name == Glob }

// Files runs file tools confined to Root.
type Files struct {
	Root string
}

// Exec runs a file tool.
func (f Files) Exec(name string, args json.RawMessage) (string, error) {
	if f.Root == "" {
		return "", errors.New("no project folder in this chat")
	}
	var a struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	switch name {
	case ReadFile:
		return f.read(a.Path)
	case Grep:
		return f.grep(a.Pattern, a.Path)
	case Glob:
		return f.glob(a.Pattern)
	default:
		return "", fmt.Errorf("unknown file tool %q", name)
	}
}

// resolve maps a relative path to an absolute one inside Root, rejecting
// escapes through .. or symlinks.
func (f Files) resolve(rel string) (string, error) {
	root, err := filepath.EvalSymlinks(f.Root)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(rel) {
		return "", errors.New("path must be relative to the project folder")
	}
	p := filepath.Join(root, rel)
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	if r, err := filepath.Rel(root, p); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the project folder")
	}
	return p, nil
}

func (f Files) read(rel string) (string, error) {
	p, err := f.resolve(rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if len(data) > maxReadBytes {
		return string(data[:maxReadBytes]) + "\n[truncated]", nil
	}
	return string(data), nil
}

func skipDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == ".realm" || name == "vendor"
}

func (f Files) grep(pattern, sub string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("bad pattern: %w", err)
	}
	start, err := f.resolve(sub)
	if err != nil {
		return "", err
	}
	root, _ := filepath.EvalSymlinks(f.Root)
	var out []string
	walkErr := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= maxMatches {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // never follow symlinks out of the project
		}
		fh, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer fh.Close()
		rel, _ := filepath.Rel(root, path)
		sc := bufio.NewScanner(fh)
		for n := 1; sc.Scan() && len(out) < maxMatches; n++ {
			if re.MatchString(sc.Text()) {
				out = append(out, fmt.Sprintf("%s:%d: %s", rel, n, sc.Text()))
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(out) == 0 {
		return "no matches", nil
	}
	return strings.Join(out, "\n"), nil
}

func (f Files) glob(pattern string) (string, error) {
	if strings.Contains(pattern, "..") || filepath.IsAbs(pattern) {
		return "", errors.New("pattern must stay inside the project folder")
	}
	root, err := filepath.EvalSymlinks(f.Root)
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "no matches", nil
	}
	rels := make([]string, 0, len(matches))
	for _, m := range matches {
		real, err := filepath.EvalSymlinks(m)
		if err != nil {
			continue
		}
		if r, err := filepath.Rel(root, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			continue // match resolves outside the project
		}
		rel, _ := filepath.Rel(root, m)
		rels = append(rels, rel)
		if len(rels) == maxMatches {
			break
		}
	}
	if len(rels) == 0 {
		return "no matches", nil
	}
	return strings.Join(rels, "\n"), nil
}
