package main

import (
	"encoding/json"
	"testing"
)

func TestIsAnthropicModel(t *testing.T) {
	tests := []struct {
		modelID string
		want    bool
	}{
		{"minimax-m2.5", true},
		{"minimax-m2.7", true},
		{"deepseek-v4-pro", false},
		{"kimi-k2.6", false},
		{"qwen3.6-plus", false},
		{"", false},
	}
	for _, tt := range tests {
		got := IsAnthropicModel(tt.modelID)
		if got != tt.want {
			t.Errorf("IsAnthropicModel(%q) = %v, want %v", tt.modelID, got, tt.want)
		}
	}
}

func TestHasThinkingBlocks(t *testing.T) {
	t.Run("with thinking blocks", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"let me think..."},{"type":"text","text":"I think..."}]`)},
		}
		if !HasThinkingBlocks(messages) {
			t.Error("expected HasThinkingBlocks to return true")
		}
	})

	t.Run("without thinking blocks", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"hello there"}]`)},
		}
		if HasThinkingBlocks(messages) {
			t.Error("expected HasThinkingBlocks to return false")
		}
	})

	t.Run("no assistant messages", func(t *testing.T) {
		messages := []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		}
		if HasThinkingBlocks(messages) {
			t.Error("expected HasThinkingBlocks to return false")
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		if HasThinkingBlocks(nil) {
			t.Error("expected HasThinkingBlocks to return false for nil")
		}
	})
}

func TestTransformRequestBasic(t *testing.T) {
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello world"`)},
		},
	}
	model := ModelConfig{
		ModelID: "deepseek-v4-pro",
	}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Model != "deepseek-v4-pro" {
		t.Errorf("expected model deepseek-v4-pro, got %s", result.Model)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("expected user role, got %s", result.Messages[0].Role)
	}
	if result.Messages[0].Content != "hello world" {
		t.Errorf("expected 'hello world', got %v", result.Messages[0].Content)
	}
}

func TestTransformRequestSystemPrompt(t *testing.T) {
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		System:    json.RawMessage(`"You are a helpful assistant."`),
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	model := ModelConfig{ModelID: "deepseek-v4-pro"}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("expected system role, got %s", result.Messages[0].Role)
	}
}

func TestTransformRequestToolUse(t *testing.T) {
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What's the weather?"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_001","name":"get_weather","input":{"location":"Beijing"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_001","content":"Sunny, 25°C"}]`)},
		},
	}
	model := ModelConfig{ModelID: "deepseek-v4-pro"}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First message: user text
	if result.Messages[0].Role != "user" || result.Messages[0].Content != "What's the weather?" {
		t.Error("first message should be user text")
	}

	// Second message: assistant with tool_call
	assistantMsg := result.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Error("second message should be assistant")
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistantMsg.ToolCalls))
	}
	if assistantMsg.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", assistantMsg.ToolCalls[0].Function.Name)
	}
	if assistantMsg.ToolCalls[0].Function.Arguments != `{"location":"Beijing"}` {
		t.Errorf("unexpected arguments: %s", assistantMsg.ToolCalls[0].Function.Arguments)
	}

	// Third message: tool result
	toolMsg := result.Messages[2]
	if toolMsg.Role != "tool" {
		t.Errorf("expected tool role, got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "toolu_001" {
		t.Errorf("expected tool_call_id toolu_001, got %s", toolMsg.ToolCallID)
	}
}

func TestTransformRequestThinking(t *testing.T) {
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"complex question"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"Let me analyze this step by step..."},{"type":"text","text":"Here is the answer."}]`)},
		},
	}
	model := ModelConfig{
		ModelID:        "deepseek-v4-pro",
		ReasoningEffort: strPtr("max"),
	}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assistant message should have reasoning_content
	assistantMsg := result.Messages[1]
	if assistantMsg.ReasoningContent == nil {
		t.Fatal("expected reasoning_content to be set")
	}
	if *assistantMsg.ReasoningContent != "Let me analyze this step by step..." {
		t.Errorf("unexpected reasoning_content: %s", *assistantMsg.ReasoningContent)
	}

	// Thinking should be enabled
	if result.Thinking == nil {
		t.Fatal("expected thinking to be set")
	}
	if string(result.Thinking) != `{"type":"enabled"}` {
		t.Errorf("unexpected thinking value: %s", string(result.Thinking))
	}
}

func TestTransformRequestDeepSeekPlaceholder(t *testing.T) {
	// DeepSeek: assistant with tool_calls but no thinking in the block MUST get
	// reasoning_content placeholder when the conversation has thinking history.
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What's the weather?"`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"thinking","thinking":"I need to check the weather."},{"type":"tool_use","id":"toolu_001","name":"get_weather","input":{"location":"Beijing"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_001","content":"Sunny, 25°C"}]`)},
			// This assistant message has tool_use but no thinking block —
			// in DeepSeek thinking mode, this requires reasoning_content placeholder.
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_002","name":"get_weather","input":{"location":"Shanghai"}}]`)},
		},
	}
	model := ModelConfig{ModelID: "deepseek-v4-pro"}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The second assistant message (tool_use without thinking) should have a placeholder
	// The first assistant message correctly has the actual thinking content.
	var assistantMsgs []ChatMessage
	for _, msg := range result.Messages {
		if msg.Role == "assistant" {
			assistantMsgs = append(assistantMsgs, msg)
		}
	}
	// First assistant: thinking + tool_use → should have real reasoning_content
	if assistantMsgs[0].ReasoningContent == nil || *assistantMsgs[0].ReasoningContent != "I need to check the weather." {
		t.Errorf("first assistant should have real reasoning content, got %v", assistantMsgs[0].ReasoningContent)
	}
	// Second assistant: tool_use only → should have placeholder
	if assistantMsgs[1].ReasoningContent == nil {
		t.Error("second assistant should have reasoning_content placeholder")
	} else if *assistantMsgs[1].ReasoningContent != " " {
		t.Errorf("expected space placeholder, got %q", *assistantMsgs[1].ReasoningContent)
	}
}

func TestTransformRequestDeepSeekDisabled(t *testing.T) {
	// No thinking in history, but config wants thinking → should send disabled
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
			{Role: "assistant", Content: json.RawMessage(`"hi there"`)},
		},
	}
	model := ModelConfig{
		ModelID:        "deepseek-v4-pro",
		Thinking:       json.RawMessage(`{"type":"enabled"}`),
		ReasoningEffort: strPtr("max"),
	}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Thinking == nil {
		t.Fatal("expected thinking to be set")
	}
	if string(result.Thinking) != `{"type":"disabled"}` {
		t.Errorf("expected thinking:disabled, got %s", string(result.Thinking))
	}
}

func TestTransformRequestConfigOverrides(t *testing.T) {
	temp := 0.3
	maxTok := 2048
	req := &MessageRequest{
		Model:       "claude-sonnet-4-20250514",
		MaxTokens:   4096,
		Temperature: float64Ptr(0.8),
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	model := ModelConfig{
		ModelID:     "deepseek-v4-pro",
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if *result.Temperature != 0.3 {
		t.Errorf("expected temperature 0.3, got %v", *result.Temperature)
	}
	if *result.MaxTokens != 2048 {
		t.Errorf("expected max_tokens 2048, got %d", *result.MaxTokens)
	}
}

func TestTransformRequestStream(t *testing.T) {
	stream := true
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Stream:    &stream,
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"hello"`)},
		},
	}
	model := ModelConfig{ModelID: "deepseek-v4-pro"}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Stream == nil || !*result.Stream {
		t.Error("expected stream to be true")
	}
	if result.StreamOptions == nil || !result.StreamOptions.IncludeUsage {
		t.Error("expected stream_options.include_usage to be true")
	}
}

func TestTransformRequestTools(t *testing.T) {
	req := &MessageRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Tools: []Tool{
			{Name: "get_weather", Description: "Get the weather", InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`)},
		},
		Messages: []Message{
			{Role: "user", Content: json.RawMessage(`"What's the weather?"`)},
		},
	}
	model := ModelConfig{ModelID: "deepseek-v4-pro"}

	result, err := TransformRequest(req, model)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Type != "function" {
		t.Errorf("expected type function, got %s", result.Tools[0].Type)
	}
	if result.Tools[0].Function.Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", result.Tools[0].Function.Name)
	}
}

func TestTransformResponseBasic(t *testing.T) {
	openaiResp := &ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Model:   "deepseek-v4-pro",
		Choices: []Choice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: ChatMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you?",
				},
			},
		},
		Usage: UsageInfo{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	resp, err := TransformResponse(openaiResp, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected original model name, got %s", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected end_turn, got %s", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("expected text block, got %s", resp.Content[0].Type)
	}
	if resp.Content[0].Text != "Hello! How can I help you?" {
		t.Errorf("unexpected text: %s", resp.Content[0].Text)
	}
}

func TestTransformResponseToolCalls(t *testing.T) {
	reasoning := "I should check the weather."
	openaiResp := &ChatCompletionResponse{
		ID:      "chatcmpl-456",
		Model:   "deepseek-v4-pro",
		Choices: []Choice{
			{
				Index:        0,
				FinishReason: "tool_calls",
				Message: ChatMessage{
					Role:             "assistant",
					ReasoningContent: &reasoning,
					ToolCalls: []ToolCall{
						{
							ID:   "call_001",
							Type: "function",
							Function: FunctionCall{
								Name:      "get_weather",
								Arguments: `{"location":"Beijing"}`,
							},
						},
					},
				},
			},
		},
		Usage: UsageInfo{
			PromptTokens:     10,
			CompletionTokens: 15,
			TotalTokens:      25,
		},
	}

	resp, err := TransformResponse(openaiResp, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.StopReason != "tool_use" {
		t.Errorf("expected tool_use, got %s", resp.StopReason)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks (thinking + tool_use), got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "thinking" {
		t.Errorf("expected thinking block first, got %s", resp.Content[0].Type)
	}
	if resp.Content[1].Type != "tool_use" {
		t.Errorf("expected tool_use block second, got %s", resp.Content[1].Type)
	}
	if resp.Content[1].Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", resp.Content[1].Name)
	}
}

func TestTransformResponseThinking(t *testing.T) {
	reasoning := "Let me think about this carefully."
	openaiResp := &ChatCompletionResponse{
		ID:      "chatcmpl-789",
		Model:   "deepseek-v4-pro",
		Choices: []Choice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: ChatMessage{
					Role:             "assistant",
					Content:          "Here is the answer.",
					ReasoningContent: &reasoning,
				},
			},
		},
		Usage: UsageInfo{},
	}

	resp, err := TransformResponse(openaiResp, "claude-opus-4-6-20250514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Content) != 2 {
		t.Fatalf("expected 2 content blocks (thinking + text), got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "thinking" {
		t.Errorf("expected thinking block first, got %s", resp.Content[0].Type)
	}
	if resp.Content[1].Type != "text" {
		t.Errorf("expected text block second, got %s", resp.Content[1].Type)
	}
	if resp.Content[0].Thinking != reasoning {
		t.Errorf("expected thinking content, got %s", resp.Content[0].Thinking)
	}
}

func TestTransformResponseFinishReasonMapping(t *testing.T) {
	tests := []struct {
		openaiReason string
		wantReason   string
	}{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"", "end_turn"},
	}

	for _, tt := range tests {
		openaiResp := &ChatCompletionResponse{
			ID:    "test",
			Model: "test",
			Choices: []Choice{
				{
					Index:        0,
					FinishReason: tt.openaiReason,
					Message: ChatMessage{
						Role:    "assistant",
						Content: "test",
					},
				},
			},
			Usage: UsageInfo{},
		}

		resp, err := TransformResponse(openaiResp, "claude-model")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StopReason != tt.wantReason {
			t.Errorf("for finish_reason %q: got %q, want %q", tt.openaiReason, resp.StopReason, tt.wantReason)
		}
	}
}

func TestTransformResponseModelUsesOriginal(t *testing.T) {
	openaiResp := &ChatCompletionResponse{
		ID:      "chatcmpl-000",
		Model:   "deepseek-v4-pro",
		Choices: []Choice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: ChatMessage{
					Role:    "assistant",
					Content: "ok",
				},
			},
		},
		Usage: UsageInfo{},
	}

	resp, err := TransformResponse(openaiResp, "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model claude-sonnet-4-20250514, got %s", resp.Model)
	}
}

func TestTransformResponseEmptyContent(t *testing.T) {
	openaiResp := &ChatCompletionResponse{
		ID:      "chatcmpl-empty",
		Model:   "test",
		Choices: []Choice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: ChatMessage{
					Role: "assistant",
				},
			},
		},
		Usage: UsageInfo{},
	}

	resp, err := TransformResponse(openaiResp, "claude-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Content) != 0 {
		t.Errorf("expected 0 content blocks, got %d", len(resp.Content))
	}
}

func TestTransformErrorResponse(t *testing.T) {
	errResp := TransformErrorResponse(400, "invalid model")
	errMap := errResp["error"].(map[string]interface{})

	if errMap["type"] != "invalid_request_error" {
		t.Errorf("expected invalid_request_error, got %s", errMap["type"])
	}

	tests := []struct {
		status   int
		errType  string
	}{
		{400, "invalid_request_error"},
		{401, "authentication_error"},
		{403, "authentication_error"},
		{404, "not_found_error"},
		{429, "rate_limit_error"},
		{500, "api_error"},
		{503, "api_error"},
	}

	for _, tt := range tests {
		resp := TransformErrorResponse(tt.status, "test")
		errMap := resp["error"].(map[string]interface{})
		if errMap["type"] != tt.errType {
			t.Errorf("status %d: expected %s, got %s", tt.status, tt.errType, errMap["type"])
		}
	}
}

// Test content string representation in messages
func TestMessageContentString(t *testing.T) {
	msg := Message{Role: "user", Content: json.RawMessage(`"hello"`)}
	blocks, err := msg.ContentBlocks()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Text != "hello" {
		t.Error("failed to parse string content")
	}
}

func TestMessageContentBlocks(t *testing.T) {
	msg := Message{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"hi"},{"type":"thinking","thinking":"hmm"}]`)}
	blocks, err := msg.ContentBlocks()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "hi" {
		t.Error("first block incorrect")
	}
	if blocks[1].Type != "thinking" || blocks[1].Thinking != "hmm" {
		t.Error("second block incorrect")
	}
}

func TestSetContentString(t *testing.T) {
	msg := &Message{Role: "user"}
	msg.SetContentString("hello world")
	if string(msg.Content) != `"hello world"` {
		t.Errorf("expected JSON string, got %s", string(msg.Content))
	}
}

func TestSetContentBlocks(t *testing.T) {
	msg := &Message{Role: "assistant"}
	msg.SetContentBlocks([]ContentBlock{
		{Type: "text", Text: "hi"},
	})
	// Single text block → stored as string
	if string(msg.Content) != `"hi"` {
		t.Errorf("expected string content, got %s", string(msg.Content))
	}

	msg.SetContentBlocks([]ContentBlock{
		{Type: "text", Text: "hi"},
		{Type: "thinking", Thinking: "hmm"},
	})
	// Multiple blocks → stored as array
	if string(msg.Content)[0] != '[' {
		t.Errorf("expected array content, got %s", string(msg.Content))
	}
}

func TestSystemText(t *testing.T) {
	req := &MessageRequest{System: json.RawMessage(`"You are helpful."`)}
	if req.SystemText() != "You are helpful." {
		t.Errorf("expected system text, got %s", req.SystemText())
	}

	req2 := &MessageRequest{System: json.RawMessage(`[{"type":"text","text":"You are helpful."}]`)}
	if req2.SystemText() != "" {
		t.Errorf("expected empty for array system, got %s", req2.SystemText())
	}
}

// Helpers
func strPtr(s string) *string { return &s }
func float64Ptr(f float64) *float64 { return &f }
