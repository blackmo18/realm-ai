// Package config resolves Realm scopes, paths and the global config file.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// EnvHome overrides the global Realm home directory (default ~/.realm).
	EnvHome = "REALM_HOME"
	// DirName is the per-project Realm directory name.
	DirName = ".realm"
	// GlobalScope is the scope name used for global bots and chats.
	GlobalScope = "global"
)

// Scope is either the global scope or one registered project.
type Scope struct {
	// Name is GlobalScope or the project slug.
	Name string
	// Root is the Realm directory: ~/.realm or <project>/.realm.
	Root string
}

// IsGlobal reports whether this is the global scope.
func (s Scope) IsGlobal() bool { return s.Name == GlobalScope }

// BotsDir is where bot folders live in this scope.
func (s Scope) BotsDir() string { return filepath.Join(s.Root, "bots") }

// DBPath is the SQLite database for this scope.
func (s Scope) DBPath() string { return filepath.Join(s.Root, "realm.db") }

// ProjectDir is the project folder for a project scope, empty for global.
func (s Scope) ProjectDir() string {
	if s.IsGlobal() {
		return ""
	}
	return filepath.Dir(s.Root)
}

// GlobalHome returns the global Realm directory.
func GlobalHome() (string, error) {
	if v := os.Getenv(EnvHome); v != "" {
		return filepath.Abs(v)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DirName), nil
}

// Global returns the global scope.
func Global() (Scope, error) {
	root, err := GlobalHome()
	if err != nil {
		return Scope{}, err
	}
	return Scope{Name: GlobalScope, Root: root}, nil
}

// ErrNoProject is returned when no initialized project encloses a directory.
var ErrNoProject = errors.New("no realm project found (run `realm init` in the project folder)")

// FindProjectRoot walks up from start looking for a .realm directory that is
// not the global home. It returns the project folder (parent of .realm).
func FindProjectRoot(start string) (string, error) {
	globalHome, err := GlobalHome()
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, DirName)
		if candidate != globalHome {
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoProject
		}
		dir = parent
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9-]+`)

// Slugify turns a folder name into a project slug.
func Slugify(name string) string {
	s := nonSlug.ReplaceAllString(strings.ToLower(name), "-")
	s = strings.Trim(s, "-")
	if s == "" || s == GlobalScope {
		s = "project-" + s
		s = strings.TrimSuffix(s, "-")
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "p-" + s
	}
	return s
}
