// Package config loads pf.json, the per-project configuration.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const FileName = "pf.json"

// Provider is any OpenAI-compatible chat completions endpoint
// (OpenAI, xAI, DeepSeek, OpenRouter, Ollama, Intern, ...).
type Provider struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env"`
	Model     string `json:"model"`
}

// Brand is a voice/compliance pack applied by `pf lint` and Pro features.
type Brand struct {
	Voice       string   `json:"voice"`
	BannedWords []string `json:"banned_words"`
}

type Config struct {
	PromptsDir      string         `json:"prompts_dir"`
	DefaultProvider string         `json:"default_provider"`
	Providers       []Provider     `json:"providers"`
	Limits          map[string]int `json:"limits"` // e.g. "amazon.title": 200
	Brand           Brand          `json:"brand"`
	Temperature     float64        `json:"temperature"`
	Root            string         `json:"-"`
}

func Default() *Config {
	return &Config{
		PromptsDir:      "prompts",
		DefaultProvider: "openai",
		Temperature:     0.7,
		Providers: []Provider{
			{Name: "openai", BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY", Model: "gpt-4o-mini"},
			{Name: "deepseek", BaseURL: "https://api.deepseek.com/v1", APIKeyEnv: "DEEPSEEK_API_KEY", Model: "deepseek-chat"},
			{Name: "xai", BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY", Model: "grok-3-mini"},
			{Name: "ollama", BaseURL: "http://localhost:11434/v1", APIKeyEnv: "", Model: "llama3.1"},
		},
		// Defaults only. Marketplaces change rules by category and region; override in pf.json.
		Limits: map[string]int{
			"amazon.title":            200,
			"amazon.bullet":           500,
			"ebay.title":              80,
			"etsy.title":              140,
			"shopify.seo_title":       70,
			"shopify.seo_description": 320,
		},
		Brand: Brand{Voice: "", BannedWords: []string{}},
	}
}

// Load walks up from the working directory to find pf.json.
func Load() (*Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		p := filepath.Join(dir, FileName)
		if b, err := os.ReadFile(p); err == nil {
			c := Default()
			if err := json.Unmarshal(b, c); err != nil {
				return nil, fmt.Errorf("parse %s: %w", p, err)
			}
			c.Root = dir
			return c, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, errors.New("no pf.json found — run `pf init` first")
		}
		dir = parent
	}
}

func (c *Config) Save(dir string) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(dir, FileName), append(b, '\n'), 0o644)
}

func (c *Config) Provider(name string) (Provider, error) {
	if name == "" {
		name = c.DefaultProvider
	}
	for _, p := range c.Providers {
		if p.Name == name {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("provider %q not in pf.json", name)
}

func (c *Config) PromptsPath() string { return filepath.Join(c.Root, c.PromptsDir) }
func (c *Config) StatePath() string   { return filepath.Join(c.Root, ".pf") }
