package main

import (
	"encoding/json"
	"fmt"
)

// ─── Anthropic types ───────────────────────────────────────────────────────────

// MessageRequest is the top-level Anthropic Messages API request.
type MessageRequest struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	System       json.RawMessage `json:"system,omitempty"`
	Messages     []Message       `json:"messages"`
	Stream       *bool           `json:"stream,omitempty"`
	Tools        []Tool          `json:"tools,omitempty"`
	Temperature  *float64        `json:"temperature,omitempty"`
	TopP         *float64        `json:"top_p,omitempty"`
	StopSequence []string        `json:"stop_sequences,omitempty"`
}

// Message represents a single message in the Anthropic conversation.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock is an individual content block within a message.
type ContentBlock struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ID         string          `json:"id,omitempty"`
	ToolUseID  string          `json:"tool_use_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Content    json.RawMessage `json:"content,omitempty"`
	IsError    *bool           `json:"is_error,omitempty"`
	Thinking   string          `json:"thinking,omitempty"`
	Signature  string          `json:"signature,omitempty"`
	Source     json.RawMessage `json:"source,omitempty"`
}

// Tool defines a tool available to the model.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// MessageResponse is the top-level Anthropic Messages API response.
type MessageResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	StopSeq    string         `json:"stop_sequence,omitempty"`
	Usage      Usage          `json:"usage"`
}

// MessageEvent is a single SSE event in the Anthropic streaming protocol.
type MessageEvent struct {
	Type         string          `json:"type"`
	Message      *MessageResponse `json:"message,omitempty"`
	Index        *int            `json:"index,omitempty"`
	ContentBlock *ContentBlock   `json:"content_block,omitempty"`
	Delta        *Delta          `json:"delta,omitempty"`
	Usage        *Usage          `json:"usage,omitempty"`
}

// Delta represents a content delta in streaming events.
type Delta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}

// Usage contains token usage information.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// SystemText extracts system prompt as a plain string.
// Returns empty string if system is an array or not set.
func (r *MessageRequest) SystemText() string {
	if len(r.System) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(r.System, &s) == nil {
		return s
	}
	return ""
}

// ContentBlocks parses message content into content blocks.
// Handles both string content ("hello") and array content ([{type:"text",...}]).
func (m *Message) ContentBlocks() ([]ContentBlock, error) {
	if len(m.Content) == 0 {
		return nil, nil
	}
	// Try string first
	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		return []ContentBlock{{Type: "text", Text: text}}, nil
	}
	// Try array
	var blocks []ContentBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal content: %w", err)
	}
	return blocks, nil
}

// SetContentString sets message content to a plain string.
func (m *Message) SetContentString(text string) {
	data, _ := json.Marshal(text)
	m.Content = data
}

// SetContentBlocks sets message content from content blocks.
func (m *Message) SetContentBlocks(blocks []ContentBlock) {
	if len(blocks) == 1 && blocks[0].Type == "text" {
		data, _ := json.Marshal(blocks[0].Text)
		m.Content = data
		return
	}
	data, _ := json.Marshal(blocks)
	m.Content = data
}

// ─── OpenAI types ──────────────────────────────────────────────────────────────

// ChatCompletionRequest is an OpenAI-compatible chat completion request.
type ChatCompletionRequest struct {
	Model            string          `json:"model"`
	Messages         []ChatMessage   `json:"messages"`
	Stream           *bool           `json:"stream,omitempty"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	ReasoningEffort  *string         `json:"reasoning_effort,omitempty"`
	Thinking         json.RawMessage `json:"thinking,omitempty"`
	Tools            []ToolDef       `json:"tools,omitempty"`
	StreamOptions    *StreamOptions  `json:"stream_options,omitempty"`
}

// ChatMessage is a single message in OpenAI format.
type ChatMessage struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content,omitempty"`
	ReasoningContent *string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall  `json:"tool_calls,omitempty"`
	Name             string      `json:"name,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
}

// ToolCall represents a tool call in OpenAI format.
type ToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name and arguments for a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef defines a tool/function available to the model.
type ToolDef struct {
	Type     string       `json:"type"`
	Function FunctionDef  `json:"function"`
}

// FunctionDef defines a function's schema.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatCompletionResponse is an OpenAI-compatible chat completion response.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   UsageInfo `json:"usage,omitempty"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message,omitempty"`
	FinishReason string      `json:"finish_reason,omitempty"`
	Delta        ChatMessage `json:"delta,omitempty"`
}

// UsageInfo contains OpenAI token usage information.
type UsageInfo struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}

// ChatCompletionChunk is a streaming chunk in OpenAI format.
type ChatCompletionChunk struct {
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	Model   string    `json:"model"`
	Choices []Choice  `json:"choices"`
	Usage   *UsageInfo `json:"usage,omitempty"`
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ─── Error response ────────────────────────────────────────────────────────────

// ErrorResponse is the Anthropic-formatted error response.
type ErrorResponse struct {
	Type  string    `json:"type"`
	Error ErrorInfo `json:"error"`
}

// ErrorInfo contains error details.
type ErrorInfo struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
