# ocgo2cli

[English](README.md) | [中文](docs/README.zh-CN.md)

Convert your [OpenCode Go](https://opencode.ai) subscription into a standard Anthropic Messages API for [Claude Code](https://github.com/anthropics/claude-code) / [Codex](https://github.com/openai/codex) (TODO).

```
OpenCode Go subscription     ocgo2cli                    Claude Code
        │                        │                            │
        │  POST /v1/chat/completions (OpenAI format)         │
        │                        │                            │
        │                        │  POST /v1/messages         │
        │                        │  {"model":"claude-sonnet-4"}│
        │                        │  ◄───────────────────────  │
        │  ◄──────────────────── │                            │
        │                        │  ────────────────────────► │
        │                        │  (Anthropic format)        │
```

**Strict 1:1 model mapping. No scenario detection, no fallback chains, no mid-conversation switching.** Just a JSON config that maps Claude model names to backend models.

## Config Example

DeepSeek V4 Pro for complex tasks (sonnet + opus), DeepSeek V4 Flash for lightweight tasks (haiku):

```json
{
  "listen": "127.0.0.1:3457",
  "opencode_base_url": "https://opencode.ai/zen/go/v1/chat/completions",
  "opencode_anthropic_base_url": "https://opencode.ai/zen/go/v1/messages",
  "api_key": "${OC_API_KEY}",
  "models": {
    "claude-sonnet-4-20250514": {
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 8192,
      "reasoning_effort": "max",
      "thinking": {"type": "enabled"}
    },
    "claude-opus-4-6-20250514": {
      "model_id": "deepseek-v4-pro",
      "temperature": 0.7,
      "max_tokens": 16384
    },
    "claude-haiku-4-5-20250514": {
      "model_id": "deepseek-v4-flash",
      "temperature": 0.5,
      "max_tokens": 4096
    }
  }
}
```

## Quick Start

```bash
git clone git@github.com:SurgeSeeker/ocgo2cli.git
cd ocgo2cli
make build

# Create config
mkdir -p ~/.config/ocgo2cli
cp config.example.json ~/.config/ocgo2cli/config.json
# Edit config.json, add your API key

# Run
./bin/ocgo2cli run        # Foreground (debugging)
./bin/ocgo2cli start      # Background daemon
```

### Claude Code Setup

```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:3457"
  }
}
```

> Note: Claude Code requires `ANTHROPIC_BASE_URL` in `settings.json` → `env`. Shell exports are ignored.

## Daemon Management

```bash
ocgo2cli start     # Start daemon
ocgo2cli stop      # Stop
ocgo2cli restart   # Restart
ocgo2cli status    # Check status
ocgo2cli install   # Install as user-level service (no sudo)
ocgo2cli uninstall # Remove service
```

Cross-platform: Linux systemd / macOS launchd / Windows SCM.

## Config Reference

| Field | Description |
|-------|-------------|
| `listen` | Listen address (default `127.0.0.1:3457`) |
| `opencode_base_url` | OpenAI-compatible endpoint |
| `opencode_anthropic_base_url` | Anthropic-native endpoint (MiniMax pass-through) |
| `api_key` | API key, supports `${ENV}` interpolation |
| `models.<name>.model_id` | Backend model ID to route to |
| `models.<name>.temperature` | Override temperature |
| `models.<name>.max_tokens` | Override max tokens |
| `models.<name>.reasoning_effort` | Reasoning effort (DeepSeek thinking mode) |
| `models.<name>.thinking` | Thinking toggle: `{"type":"enabled"}` / `{"type":"disabled"}` |

## Features

- **Anthropic ↔ OpenAI format conversion** — full type fidelity, thinking/reasoning_content roundtrip
- **Anthropic-native bypass** — MiniMax M2.5/M2.7 pass through with zero conversion
- **SSE streaming** — real-time with proper reasoning/text block transitions
- **`${ENV}` interpolation** — keep API keys out of git
- **Cross-platform daemon** — kardianos/service, user-level, no root

## Build & Test

```bash
make build    # Build → bin/ocgo2cli
make test     # Run tests with race detector
make lint     # go vet
make clean    # Clean artifacts
```

## Docs

- [中文文档](docs/README.zh-CN.md)
- [CLAUDE.md](CLAUDE.md) — project architecture for Claude Code

## Credits

Format conversion based on [oc-go-cc](https://github.com/nousresearch/oc-go-cc) by Nous Research.

## License

GNU Affero General Public License v3.0 — see [LICENSE](LICENSE).
