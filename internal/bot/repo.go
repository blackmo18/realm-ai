package bot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/blackmo18/realm-ai/internal/config"
)

// ErrNotFound is returned when a bot does not exist in a scope.
var ErrNotFound = errors.New("bot not found")

const fileName = "bot.yaml"

// Repo stores bots of one scope as folders under <scope>/bots.
type Repo struct {
	scope config.Scope
}

// NewRepo returns a repository for a scope.
func NewRepo(scope config.Scope) *Repo { return &Repo{scope: scope} }

// Scope returns the repository's scope.
func (r *Repo) Scope() config.Scope { return r.scope }

func (r *Repo) dir(name string) string { return filepath.Join(r.scope.BotsDir(), name) }

// KnowledgeDir is the bot's knowledge folder.
func KnowledgeDir(b Bot) string { return filepath.Join(b.Dir, "knowledge") }

// WorkspaceDir is the working directory used by global bots.
func WorkspaceDir(b Bot) string { return filepath.Join(b.Dir, "workspace") }

// Create writes a new bot folder. It fails if the bot already exists.
func (r *Repo) Create(b Bot) (Bot, error) {
	nb := b.WithDefaults()
	nb.Scope = r.scope.Name
	nb.Dir = r.dir(nb.Name)
	if err := nb.Validate(); err != nil {
		return Bot{}, err
	}
	if _, err := os.Stat(nb.Dir); err == nil {
		return Bot{}, fmt.Errorf("bot %s already exists", nb.Handle())
	}
	for _, d := range []string{KnowledgeDir(nb), WorkspaceDir(nb)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return Bot{}, fmt.Errorf("create bot folder: %w", err)
		}
	}
	if err := r.Save(nb); err != nil {
		return Bot{}, err
	}
	return nb, nil
}

// Get loads one bot.
func (r *Repo) Get(name string) (Bot, error) {
	path := filepath.Join(r.dir(name), fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Bot{}, fmt.Errorf("%w: @%s/%s", ErrNotFound, r.scope.Name, name)
	}
	if err != nil {
		return Bot{}, fmt.Errorf("read %s: %w", path, err)
	}
	var b Bot
	if err := yaml.Unmarshal(data, &b); err != nil {
		return Bot{}, fmt.Errorf("parse %s: %w", path, err)
	}
	b = b.WithDefaults()
	b.Scope = r.scope.Name
	b.Dir = r.dir(name)
	return b, nil
}

// List returns all bots in the scope sorted by name.
func (r *Repo) List() ([]Bot, error) {
	entries, err := os.ReadDir(r.scope.BotsDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list bots: %w", err)
	}
	bots := make([]Bot, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := r.Get(e.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		bots = append(bots, b)
	}
	sort.Slice(bots, func(i, j int) bool { return bots[i].Name < bots[j].Name })
	return bots, nil
}

// Save validates and atomically writes bot.yaml.
func (r *Repo) Save(b Bot) error {
	if err := b.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(b)
	if err != nil {
		return fmt.Errorf("encode bot: %w", err)
	}
	dir := r.dir(b.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create bot folder: %w", err)
	}
	tmp := filepath.Join(dir, fileName+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write bot: %w", err)
	}
	return os.Rename(tmp, filepath.Join(dir, fileName))
}

// Apply runs a lifecycle action and saves the result.
func (r *Repo) Apply(name, action string) (Bot, error) {
	b, err := r.Get(name)
	if err != nil {
		return Bot{}, err
	}
	next, err := Transition(b.Status, action)
	if err != nil {
		return Bot{}, fmt.Errorf("%s: %w", b.Handle(), err)
	}
	updated := b
	updated.Status = next
	if err := r.Save(updated); err != nil {
		return Bot{}, err
	}
	return updated, nil
}

// Update loads a bot, applies change to a copy, and saves it.
func (r *Repo) Update(name string, change func(Bot) Bot) (Bot, error) {
	b, err := r.Get(name)
	if err != nil {
		return Bot{}, err
	}
	updated := change(b)
	updated.Name, updated.Scope, updated.Dir = b.Name, b.Scope, b.Dir
	if err := r.Save(updated); err != nil {
		return Bot{}, err
	}
	return updated, nil
}
