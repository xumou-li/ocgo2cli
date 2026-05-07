# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## Commands

```bash
make build   # Build binary to bin/ocgo2cli
make test    # Run tests with race detector (-race)
make lint    # go vet
make clean   # Remove build artifacts
```

## Architecture

**Purpose:** ocgo2cli is a lightweight HTTP proxy that maps Claude model names to OpenCode Go models with a strict 1:1 mapping. No scenario detection, no fallback chains, no circuit breakers.

**Core principle:** Claude Code says "claude-sonnet-4" → always routes to the configured backend model. No mid-conversation model switching.

## Key Design Decisions

- **Strict 1:1 model mapping** — config-driven, no message content analysis
- **Anthropic-native models** (MiniMax M2.5/M2.7) bypass format conversion entirely → raw `/v1/messages` forwarding
- **Thinking blocks preserved** — `thinking` ↔ `reasoning_content` roundtrip for DeepSeek compatibility
- **No retry, no fallback, no circuit breaker** — failed requests pass through to Claude Code
- **KISS** — 5 files, ~1400 lines, standard library only

## File Structure

```
main.go         — HTTP server, handler, model routing logic
config.go       — JSON config loader with ${ENV} interpolation
transformer.go  — Anthropic ↔ OpenAI format conversion
types.go        — Anthropic + OpenAI type definitions
sse.go          — Streaming SSE transformation
```

## Model Routing

```
Request arrives POST /v1/messages {"model": "claude-sonnet-4-20250514", ...}
  → Lookup config.models["claude-sonnet-4-20250514"] → ModelConfig
  → IsAnthropicModel(model_id)?
    YES → replace model in raw body → POST /v1/messages (Anthropic format, pass-through)
    NO  → transform Anthropic→OpenAI → POST /v1/chat/completions → transform back
  → Return response with original Claude model name in "model" field
```

## Anthropic-native model detection (hardcoded)

```go
func IsAnthropicModel(modelID string) bool {
    switch modelID {
    case "minimax-m2.5", "minimax-m2.7":
        return true
    }
    return false
}
```

## Thinking/Reasoning Handling (DeepSeek critical)

1. When transforming Anthropic→OpenAI, detect if message history has `thinking` blocks via `HasThinkingBlocks()`
2. If history has thinking → send `thinking: {"type":"enabled"}` + `reasoning_effort`
3. If history lacks thinking but model config wants it → send `thinking: {"type":"disabled"}` (prevents 400)
4. DeepSeek in thinking mode requires `reasoning_content` on ALL assistant messages (including tool_calls). Insert placeholder `" "` when missing.
5. When transforming OpenAI→Anthropic, `reasoning_content` → `thinking` block
6. Streaming: `reasoning_content` delta → `thinking_delta` SSE; proper block start/stop transitions between reasoning and text

## Configuration

Config file: `~/.config/ocgo2cli/config.json`
- `${VAR}` environment variable interpolation
- Per-model temperature, max_tokens, reasoning_effort, thinking overrides

## Format Conversion Reference

This project's transformer code references [oc-go-cc](https://github.com/nousresearch/oc-go-cc):
- `internal/transformer/request.go` — Anthropic→OpenAI conversion
- `internal/transformer/response.go` — OpenAI→Anthropic conversion
- `internal/transformer/stream.go` — SSE streaming conversion
- `pkg/types/anthropic.go` — Polymorphic field handling (system/content as string or array)
- `pkg/types/openai.go` — OpenAI type definitions
- `internal/client/opencode.go` — Anthropic-native model endpoint routing

## Format Test Results (validated against live OpenCode Go API)

- OpenAI API: `reasoning_content` exists alongside `content` in response. Tool calls in `tool_calls[]` array with `function.name`, `function.arguments` (JSON string), `id`, `index`.
- Streaming: Chunks come as `data: {...}\n\n`. Delta has `content`, `reasoning_content`, `tool_calls[]`. `finish_reason` in final chunk. `usage` in separate chunk with `include_usage: true`.
- DeepSeek thinking mode: Missing `reasoning_content` on tool-call assistant messages → 400 "must be passed back to the API". Adding `"reasoning_content": " "` fixes it.
- MiniMax Anthropic: Standard Anthropic format response. Extra fields: `base_resp`, `cost`.
- DeepSeek usage: `prompt_cache_hit_tokens`, `prompt_cache_miss_tokens` present. Other models may omit.
- Qwen models: Always output reasoning_content by default even without `thinking: enabled`.
- All tool calls: `finish_reason: "tool_calls"` in OpenAI, maps to `stop_reason: "tool_use"` in Anthropic.

## Commit Convention

```
feat(scope): description

```
