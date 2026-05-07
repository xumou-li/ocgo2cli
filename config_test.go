package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigBasic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	configJSON := `{
		"listen": "127.0.0.1:9999",
		"opencode_base_url": "https://example.com/v1/chat/completions",
		"opencode_anthropic_base_url": "https://example.com/v1/messages",
		"api_key": "sk-test-key",
		"models": {
			"claude-sonnet-4-20250514": {
				"model_id": "deepseek-v4-pro",
				"temperature": 0.7,
				"max_tokens": 8192
			}
		}
	}`
	os.WriteFile(configPath, []byte(configJSON), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Listen != "127.0.0.1:9999" {
		t.Errorf("expected listen 127.0.0.1:9999, got %s", cfg.Listen)
	}
	if cfg.APIKey != "sk-test-key" {
		t.Errorf("expected api_key sk-test-key, got %s", cfg.APIKey)
	}
	if len(cfg.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(cfg.Models))
	}
	mc, ok := cfg.Models["claude-sonnet-4-20250514"]
	if !ok {
		t.Fatal("expected model config for claude-sonnet-4-20250514")
	}
	if mc.ModelID != "deepseek-v4-pro" {
		t.Errorf("expected deepseek-v4-pro, got %s", mc.ModelID)
	}
	if mc.Temperature == nil || *mc.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", mc.Temperature)
	}
}

func TestLoadConfigEnvInterpolation(t *testing.T) {
	os.Setenv("TEST_OC_API_KEY", "sk-env-key")
	defer os.Unsetenv("TEST_OC_API_KEY")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	configJSON := `{
		"listen": "127.0.0.1:3457",
		"opencode_base_url": "https://example.com/v1/chat/completions",
		"opencode_anthropic_base_url": "https://example.com/v1/messages",
		"api_key": "${TEST_OC_API_KEY}",
		"models": {}
	}`
	os.WriteFile(configPath, []byte(configJSON), 0644)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.APIKey != "sk-env-key" {
		t.Errorf("expected api_key sk-env-key from env, got %s", cfg.APIKey)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != "127.0.0.1:3457" {
		t.Errorf("expected default listen, got %s", cfg.Listen)
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	os.WriteFile(configPath, []byte(`{invalid json`), 0644)

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestConfigValidate(t *testing.T) {
	t.Run("missing api_key", func(t *testing.T) {
		cfg := &Config{
			Listen: "127.0.0.1:3457",
			Models: map[string]ModelConfig{
				"test": {ModelID: "test-model"},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing api_key")
		}
	})

	t.Run("no models", func(t *testing.T) {
		cfg := &Config{
			Listen: "127.0.0.1:3457",
			APIKey: "sk-key",
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for empty models")
		}
	})

	t.Run("missing model_id", func(t *testing.T) {
		cfg := &Config{
			Listen: "127.0.0.1:3457",
			APIKey: "sk-key",
			Models: map[string]ModelConfig{
				"test": {},
			},
		}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for missing model_id")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			Listen: "127.0.0.1:3457",
			APIKey: "sk-key",
			Models: map[string]ModelConfig{
				"test": {ModelID: "test-model"},
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Listen != "127.0.0.1:3457" {
		t.Errorf("expected default listen, got %s", cfg.Listen)
	}
	if cfg.Models == nil {
		t.Error("expected non-nil models map")
	}
}

func TestSaveDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	err := SaveDefaultConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was created
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("could not read config: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Verify not overwriting existing
	err = SaveDefaultConfig(configPath)
	if err != nil {
		t.Fatalf("unexpected error on second save: %v", err)
	}
}

func TestModelConfigFields(t *testing.T) {
	mc := ModelConfig{
		ModelID:        "deepseek-v4-pro",
		Temperature:    float64Ptr(0.5),
		MaxTokens:      intPtr(4096),
		ReasoningEffort: strPtr("medium"),
		Thinking:       json.RawMessage(`{"type":"enabled"}`),
	}

	if mc.ModelID != "deepseek-v4-pro" {
		t.Errorf("expected deepseek-v4-pro")
	}
	if mc.Temperature == nil || *mc.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5")
	}
	if mc.MaxTokens == nil || *mc.MaxTokens != 4096 {
		t.Errorf("expected max_tokens 4096")
	}
}

func TestResolveEnv(t *testing.T) {
	os.Setenv("TEST_RESOLVE", "resolved-value")
	defer os.Unsetenv("TEST_RESOLVE")

	if got := resolveEnv("${TEST_RESOLVE}"); got != "resolved-value" {
		t.Errorf("expected resolved-value, got %s", got)
	}
	if got := resolveEnv("plain-value"); got != "plain-value" {
		t.Errorf("expected plain-value, got %s", got)
	}
}
