package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config holds the full application configuration.
type Config struct {
	Listen                 string                `json:"listen"`
	OpenCodeBaseURL        string                `json:"opencode_base_url"`
	OpenCodeAnthropicBaseURL string              `json:"opencode_anthropic_base_url"`
	APIKey                 string                `json:"api_key"`
	RequestTimeout         int                   `json:"request_timeout_seconds"`
	MaxIdleConns           int                   `json:"max_idle_connections"`
	Models                 map[string]ModelConfig `json:"models"`
}

// ModelConfig maps a Claude model name to a backend model and its parameters.
type ModelConfig struct {
	ModelID        string          `json:"model_id"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	ReasoningEffort *string        `json:"reasoning_effort,omitempty"`
	Thinking       json.RawMessage `json:"thinking,omitempty"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() *Config {
	return &Config{
		Listen:                   "127.0.0.1:3457",
		OpenCodeBaseURL:          "https://opencode.ai/zen/go/v1/chat/completions",
		OpenCodeAnthropicBaseURL: "https://opencode.ai/zen/go/v1/messages",
		APIKey:                   "",
		RequestTimeout:           300,
		MaxIdleConns:             20,
		Models:                   make(map[string]ModelConfig),
	}
}

// LoadConfig reads and parses the JSON config file, interpolating ${VAR}
// references with environment variable values.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}

	raw := interpolateEnv(string(data))

	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:3457"
	}
	if cfg.Models == nil {
		cfg.Models = make(map[string]ModelConfig)
	}

	return &cfg, nil
}

var envVarRe = regexp.MustCompile(`\$\{(\w+)\}`)

func interpolateEnv(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		// match is like "${VAR_NAME}"
		varName := match[2 : len(match)-1]
		return os.Getenv(varName)
	})
}

// ConfigDir returns the configuration directory (~/.config/ocgo2cli).
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".config", "ocgo2cli"), nil
}

// DefaultConfigPath returns the default path to config.json.
func DefaultConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// SaveDefaultConfig writes the default config file if it does not exist.
func SaveDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	data, _ := json.MarshalIndent(DefaultConfig(), "", "  ")
	return os.WriteFile(path, data, 0644)
}

// Validate checks the config for required fields.
func (c *Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}
	if len(c.Models) == 0 {
		return fmt.Errorf("at least one model mapping is required")
	}
	for claudeModel, mc := range c.Models {
		if mc.ModelID == "" {
			return fmt.Errorf("model_id is required for %s", claudeModel)
		}
	}
	return nil
}

// resolveEnv is a helper to unwrap ${VAR} when printing configs.
func resolveEnv(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return os.Getenv(s[2 : len(s)-1])
	}
	return s
}
