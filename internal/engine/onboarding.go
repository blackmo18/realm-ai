package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/blackmo18/realm-ai/internal/config"
	"github.com/blackmo18/realm-ai/internal/store"
)

// FolderInfo is what Realm finds in a candidate project folder.
type FolderInfo struct {
	Path        string `json:"path"`
	Exists      bool   `json:"exists"`
	IsGit       bool   `json:"is_git"`
	HasClaudeMD bool   `json:"has_claude_md"`
	SkillCount  int    `json:"skill_count"`
	Registered  bool   `json:"registered"`
	Slug        string `json:"slug"`
}

// ExpandPath resolves a leading ~ and makes the path absolute.
func ExpandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("folder path is empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Abs(p)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// InspectFolder reports what a folder holds before registering it.
func (e *Engine) InspectFolder(path string) (FolderInfo, error) {
	abs, err := ExpandPath(path)
	if err != nil {
		return FolderInfo{}, err
	}
	info := FolderInfo{Path: abs, Exists: isDir(abs), Slug: config.Slugify(filepath.Base(abs))}
	if !info.Exists {
		return info, nil
	}
	info.IsGit = isDir(filepath.Join(abs, ".git"))
	_, statErr := os.Stat(filepath.Join(abs, "CLAUDE.md"))
	info.HasClaudeMD = statErr == nil
	info.SkillCount = len(readSkills(filepath.Join(abs, ".claude", "skills"), "project"))
	gs, err := e.store(config.GlobalScope)
	if err != nil {
		return FolderInfo{}, err
	}
	if p, err := gs.ProjectByRoot(abs); err == nil {
		info.Registered, info.Slug = true, p.Slug
	}
	return info, nil
}

// suggestRoots are common places people keep code, relative to home.
var suggestRoots = []string{"code", "Code", "src", "dev", "Projects", "projects", "Documents/Projects", "Documents/GitHub", "repos", "work"}

const (
	suggestDepth   = 3
	suggestMaxDirs = 5000
)

// SuggestFolders finds git folders under common code locations that are not
// registered yet. The scan is bounded in depth and directory count.
func (e *Engine) SuggestFolders(limit int) ([]string, error) {
	if limit <= 0 {
		limit = 8
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	projects, err := e.Projects()
	if err != nil {
		return nil, err
	}
	registered := map[string]bool{}
	for _, p := range projects {
		registered[p.Root] = true
	}
	var out []string
	visited := 0
	for _, r := range suggestRoots {
		root := filepath.Join(home, r)
		if !isDir(root) {
			continue
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			visited++
			if len(out) >= limit || visited > suggestMaxDirs {
				return filepath.SkipAll
			}
			rel, _ := filepath.Rel(root, p)
			if strings.Count(rel, string(filepath.Separator)) >= suggestDepth || strings.HasPrefix(d.Name(), ".") && p != root || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if isDir(filepath.Join(p, ".git")) {
				if !registered[p] && !slices.Contains(out, p) {
					out = append(out, p)
				}
				return filepath.SkipDir
			}
			return nil
		})
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// RegisterProjectAs registers dir under a chosen slug (empty = from the
// folder name) and can keep the chat database out of git.
func (e *Engine) RegisterProjectAs(dir, slug string, ignoreDB bool) (config.Scope, error) {
	abs, err := ExpandPath(dir)
	if err != nil {
		return config.Scope{}, err
	}
	if !isDir(abs) {
		return config.Scope{}, fmt.Errorf("%s is not a folder", abs)
	}
	sc, err := e.registerProject(abs, slug)
	if err != nil {
		return config.Scope{}, err
	}
	if ignoreDB {
		if err := ensureGitignore(abs, config.DirName+"/realm.db*"); err != nil {
			return sc, fmt.Errorf("registered %s but could not update .gitignore: %w", sc.Name, err)
		}
	}
	return sc, nil
}

func ensureGitignore(dir, line string) error {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for l := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(prefix + line + "\n")
	return err
}

// Template is a starter bot offered during onboarding.
type Template struct {
	Name    string   `json:"name"`
	Model   string   `json:"model"`
	Effort  string   `json:"effort"`
	Charter string   `json:"charter"`
	Topics  []string `json:"topics"`
}

// Templates returns the starter bots offered when adding a project.
func Templates() []Template {
	return []Template{
		{Name: "reviewer", Model: "sonnet", Effort: "high", Topics: []string{"code-review"},
			Charter: "Reviews code in this repo for correctness, security and style. Refuses deploys and database schema work."},
		{Name: "docs", Model: "haiku", Topics: []string{"docs", "readme", "adr"},
			Charter: "Keeps the README, ADRs and changelog accurate and current. Refuses code changes outside documentation."},
		{Name: "tester", Model: "sonnet", Topics: []string{"tests", "testing"},
			Charter: "Writes and fixes tests for this repo and explains failures. Refuses work outside the test suite."},
	}
}

// OnboardInput registers a project, creates starter bots and a first chat.
type OnboardInput struct {
	Dir           string   `json:"dir"`
	Slug          string   `json:"slug"`
	IgnoreDB      bool     `json:"ignore_db"`
	Starters      []string `json:"starters"`
	GlobalMembers []string `json:"global_members"`
	ChatName      string   `json:"chat_name"`
	Routing       string   `json:"routing"`
}

// OnboardResult reports what onboarding created.
type OnboardResult struct {
	Project string     `json:"project"`
	Root    string     `json:"root"`
	Bots    []string   `json:"bots"`
	Chat    store.Chat `json:"chat"`
}

// Onboard runs the whole Add project flow.
func (e *Engine) Onboard(in OnboardInput) (OnboardResult, error) {
	sc, err := e.RegisterProjectAs(in.Dir, in.Slug, in.IgnoreDB)
	if err != nil {
		return OnboardResult{}, err
	}
	res := OnboardResult{Project: sc.Name, Root: sc.ProjectDir(), Bots: []string{}}
	templates := Templates()
	for _, name := range in.Starters {
		i := slices.IndexFunc(templates, func(t Template) bool { return t.Name == name })
		if i < 0 {
			return res, fmt.Errorf("unknown starter bot %q", name)
		}
		t := templates[i]
		v, err := e.CreateBot(BotInput{Scope: sc.Name, Name: t.Name, Model: t.Model, Effort: t.Effort, Charter: t.Charter, Topics: t.Topics})
		if err != nil {
			return res, err
		}
		res.Bots = append(res.Bots, v.Handle)
	}
	name := in.ChatName
	if name == "" {
		name = "general"
	}
	chat, err := e.CreateChat(sc.Name, name, in.Routing, in.GlobalMembers)
	if err != nil {
		return res, err
	}
	res.Chat = chat
	return res, nil
}
