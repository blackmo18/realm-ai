package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Provider types for API-key runtimes.
const (
	ProviderAnthropic    = "anthropic"
	ProviderOpenAICompat = "openai-compat"
)

// ProviderConfig describes an API-key provider. Keys are read from the
// environment variable named in APIKeyEnv; they are never stored in files.
type ProviderConfig struct {
	Type      string `yaml:"type"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env,omitempty"`
}

// APIKey reads the key from the configured environment variable. Providers
// without APIKeyEnv (for example a local Ollama) return an empty key.
func (p ProviderConfig) APIKey() (string, error) {
	if p.APIKeyEnv == "" {
		return "", nil
	}
	key := os.Getenv(p.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set", p.APIKeyEnv)
	}
	return key, nil
}

// Limits bound how much work the daemon does.
type Limits struct {
	MaxConcurrent   int `yaml:"max_concurrent"`
	MaxHops         int `yaml:"max_hops"`
	HistoryMessages int `yaml:"history_messages"`
	TurnTimeoutSec  int `yaml:"turn_timeout_sec"`
}

// Config is ~/.realm/config.yaml.
type Config struct {
	ClaudeBin      string                    `yaml:"claude_bin"`
	ModeratorModel string                    `yaml:"moderator_model"`
	Providers      map[string]ProviderConfig `yaml:"providers"`
	Limits         Limits                    `yaml:"limits"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		ClaudeBin:      "claude",
		ModeratorModel: "haiku",
		Providers: map[string]ProviderConfig{
			"anthropic":  {Type: ProviderAnthropic, BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY"},
			"openai":     {Type: ProviderOpenAICompat, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
			"openrouter": {Type: ProviderOpenAICompat, BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY"},
			"ollama":     {Type: ProviderOpenAICompat, BaseURL: "http://localhost:11434/v1"},
		},
		Limits: Limits{MaxConcurrent: 3, MaxHops: 4, HistoryMessages: 20, TurnTimeoutSec: 600},
	}
}

// Path returns the config file path inside a global home.
func Path(globalHome string) string { return filepath.Join(globalHome, "config.yaml") }

// Load reads the config file, falling back to defaults for a missing file or
// unset fields.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var file Config
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	merged := merge(cfg, file)
	if err := merged.Validate(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return merged, nil
}

func merge(base, file Config) Config {
	out := base
	if file.ClaudeBin != "" {
		out.ClaudeBin = file.ClaudeBin
	}
	if file.ModeratorModel != "" {
		out.ModeratorModel = file.ModeratorModel
	}
	providers := make(map[string]ProviderConfig, len(base.Providers)+len(file.Providers))
	for k, v := range base.Providers {
		providers[k] = v
	}
	for k, v := range file.Providers {
		providers[k] = v
	}
	out.Providers = providers
	out.Limits = mergeLimits(base.Limits, file.Limits)
	return out
}

func mergeLimits(base, file Limits) Limits {
	out := base
	if file.MaxConcurrent > 0 {
		out.MaxConcurrent = file.MaxConcurrent
	}
	if file.MaxHops > 0 {
		out.MaxHops = file.MaxHops
	}
	if file.HistoryMessages > 0 {
		out.HistoryMessages = file.HistoryMessages
	}
	if file.TurnTimeoutSec > 0 {
		out.TurnTimeoutSec = file.TurnTimeoutSec
	}
	return out
}

// Validate checks provider definitions.
func (c Config) Validate() error {
	for name, p := range c.Providers {
		if name == "claude-cli" {
			return errors.New(`provider name "claude-cli" is reserved`)
		}
		if p.Type != ProviderAnthropic && p.Type != ProviderOpenAICompat {
			return fmt.Errorf("provider %q: unknown type %q (want %s or %s)", name, p.Type, ProviderAnthropic, ProviderOpenAICompat)
		}
		if p.BaseURL == "" {
			return fmt.Errorf("provider %q: base_url is required", name)
		}
	}
	return nil
}

// WriteDefault writes the default config if none exists.
func WriteDefault(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	data, err := yaml.Marshal(Default())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
