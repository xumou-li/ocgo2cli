package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// IsAnthropicModel returns true if the backend model speaks the Anthropic protocol natively.
func IsAnthropicModel(modelID string) bool {
	switch modelID {
	case "minimax-m2.5", "minimax-m2.7":
		return true
	}
	return false
}

// HasThinkingBlocks checks whether any assistant message in the history
// contains a thinking content block.
func HasThinkingBlocks(messages []Message) bool {
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		blocks, err := msg.ContentBlocks()
		if err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "thinking" {
				return true
			}
		}
	}
	return false
}

// TransformRequest converts an Anthropic MessageRequest to an OpenAI ChatCompletionRequest.
func TransformRequest(anthropicReq *MessageRequest, model ModelConfig) (*ChatCompletionRequest, error) {
	sysText := anthropicReq.SystemText()

	var openaiMessages []ChatMessage

	// Add system message first if present
	if sysText != "" {
		openaiMessages = append(openaiMessages, ChatMessage{
			Role:    "system",
			Content: sysText,
		})
	}

	hasThinkingInHistory := HasThinkingBlocks(anthropicReq.Messages)

	for _, msg := range anthropicReq.Messages {
		transformed, err := transformMessage(msg, hasThinkingInHistory)
		if err != nil {
			return nil, fmt.Errorf("transform message: %w", err)
		}
		openaiMessages = append(openaiMessages, transformed...)
	}

	// Build request
	req := &ChatCompletionRequest{
		Model:       model.ModelID,
		Messages:    openaiMessages,
		Temperature: anthropicReq.Temperature,
		TopP:        anthropicReq.TopP,
	}

	if anthropicReq.MaxTokens > 0 {
		mt := anthropicReq.MaxTokens
		req.MaxTokens = &mt
	}

	if anthropicReq.Stream != nil && *anthropicReq.Stream {
		req.Stream = anthropicReq.Stream
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
	}

	// Apply model config overrides
	if model.Temperature != nil {
		req.Temperature = model.Temperature
	}
	if model.MaxTokens != nil {
		req.MaxTokens = model.MaxTokens
	}

	// Transform tools
	if len(anthropicReq.Tools) > 0 {
		for _, t := range anthropicReq.Tools {
			req.Tools = append(req.Tools, ToolDef{
				Type: "function",
				Function: FunctionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	// Handle thinking/reasoning_effort
	if model.ForceThinkingDisable != nil && *model.ForceThinkingDisable {
		req.Thinking = json.RawMessage(`{"type":"disabled"}`)
	} else if hasThinkingInHistory {
		req.Thinking = json.RawMessage(`{"type":"enabled"}`)
		if model.ReasoningEffort != nil {
			req.ReasoningEffort = model.ReasoningEffort
		}
	} else if model.Thinking != nil || model.ReasoningEffort != nil {
		// Model config wants thinking but no thinking in history —
		// must send disabled or DeepSeek/Qwen may reject.
		req.Thinking = json.RawMessage(`{"type":"disabled"}`)
	}
	} else if model.Thinking != nil || model.ReasoningEffort != nil {
		// Model config wants thinking but no thinking in history —
		// must send disabled or DeepSeek/Qwen may reject.
		// When forcing disabled, do NOT include reasoning_effort —
		// DeepSeek rejects the combination.
		req.Thinking = json.RawMessage(`{"type":"disabled"}`)
	}

	return req, nil
}

// transformMessage converts a single Anthropic message to one or more OpenAI messages.
func transformMessage(msg Message, hasThinkingInHistory bool) ([]ChatMessage, error) {
	blocks, err := msg.ContentBlocks()
	if err != nil {
		return nil, err
	}

	switch msg.Role {
	case "user":
		return transformUserMessage(blocks)
	case "assistant":
		return transformAssistantMessage(blocks, hasThinkingInHistory)
	default:
		return nil, fmt.Errorf("unknown message role: %s", msg.Role)
	}
}

func transformUserMessage(blocks []ContentBlock) ([]ChatMessage, error) {
	var result []ChatMessage

	// Collect text parts and tool results
	var textParts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "tool_result":
			// Content can be a JSON string or an array of content blocks.
			// Unmarshal to strip JSON quotes before passing through.
			var contentStr string
			if b.Content != nil {
				if err := json.Unmarshal(b.Content, &contentStr); err != nil {
					// If not a plain string, pass raw content through
					contentStr = string(b.Content)
				}
			}
			result = append(result, ChatMessage{
				Role:       "tool",
				Content:    contentStr,
				ToolCallID: b.ToolUseID,
			})
		case "image":
			// Images need to be passed as content array
			result = append(result, ChatMessage{
				Role:    "user",
				Content: []map[string]interface{}{
					{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": imageSourceURL(b.Source)},
					},
				},
			})
		}
	}

	if len(textParts) > 0 {
		// Prepend text user message before tool results
		result = append([]ChatMessage{{
			Role:    "user",
			Content: strings.Join(textParts, "\n"),
		}}, result...)
	}

	return result, nil
}

func imageSourceURL(source json.RawMessage) string {
	var s struct {
		Type string `json:"type"`
		URL  string `json:"url"`
		Data string `json:"data"`
	}
	if json.Unmarshal(source, &s) != nil {
		return ""
	}
	if s.URL != "" {
		return s.URL
	}
	if s.Data != "" {
		return "data:" + s.Type + ";base64," + s.Data
	}
	return ""
}

func transformAssistantMessage(blocks []ContentBlock, hasThinkingInHistory bool) ([]ChatMessage, error) {
	msg := ChatMessage{Role: "assistant"}

	var textParts []string

	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "thinking":
			if b.Thinking != "" {
				msg.ReasoningContent = &b.Thinking
			}
		case "tool_use":
			tc := ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      b.Name,
					Arguments: string(b.Input),
				},
			}
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
	}

	if len(textParts) > 0 {
		msg.Content = strings.Join(textParts, "\n")
	}

	// DeepSeek critical fix: if thinking mode is used, every assistant message
	// with tool_calls MUST have reasoning_content, even if empty placeholder.
	// Failing to include it causes 400: "The reasoning_content in the thinking
	// mode must be passed back to the API."
	if hasThinkingInHistory && len(msg.ToolCalls) > 0 && msg.ReasoningContent == nil {
		placeholder := " "
		msg.ReasoningContent = &placeholder
	}

	return []ChatMessage{msg}, nil
}

// TransformResponse converts an OpenAI ChatCompletionResponse back to Anthropic format.
func TransformResponse(openaiResp *ChatCompletionResponse, originalModel string) (*MessageResponse, error) {
	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := openaiResp.Choices[0]
	msg := choice.Message

	var content []ContentBlock

	// 1. reasoning_content → thinking block (placed before text)
	if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
		content = append(content, ContentBlock{
			Type:     "thinking",
			Thinking: *msg.ReasoningContent,
		})
	}

	// 2. content → text block
	if msg.Content != nil {
		switch v := msg.Content.(type) {
		case string:
			if v != "" {
				content = append(content, ContentBlock{
					Type: "text",
					Text: v,
				})
			}
		case []interface{}:
			// content array — handle each part
			for _, part := range v {
				partMap, ok := part.(map[string]interface{})
				if !ok {
					continue
				}
				partType, _ := partMap["type"].(string)
				switch partType {
				case "text":
					text, _ := partMap["text"].(string)
					content = append(content, ContentBlock{
						Type: "text",
						Text: text,
					})
				}
			}
		}
	}

	// 3. tool_calls → tool_use blocks
	for _, tc := range msg.ToolCalls {
		input := json.RawMessage(tc.Function.Arguments)
		content = append(content, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	// 4. finish_reason mapping
	stopReason := mapFinishReason(choice.FinishReason)

	resp := &MessageResponse{
		ID:         openaiResp.ID,
		Type:       "message",
		Role:       "assistant",
		Content:    content,
		Model:      originalModel, // use the original Claude model name
		StopReason: stopReason,
		Usage: Usage{
			InputTokens:              openaiResp.Usage.PromptTokens,
			OutputTokens:             openaiResp.Usage.CompletionTokens,
			CacheCreationInputTokens: openaiResp.Usage.PromptCacheMissTokens,
			CacheReadInputTokens:     openaiResp.Usage.PromptCacheHitTokens,
		},
	}

	return resp, nil
}

func mapFinishReason(finishReason string) string {
	switch finishReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		if finishReason == "" {
			return "end_turn"
		}
		return finishReason
	}
}

// TransformErrorResponse creates an Anthropic-formatted error response.
func TransformErrorResponse(statusCode int, message string) map[string]interface{} {
	errorType := mapHTTPToAnthropicError(statusCode)
	return map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errorType,
			"message": message,
		},
	}
}

func mapHTTPToAnthropicError(statusCode int) string {
	switch {
	case statusCode == 400:
		return "invalid_request_error"
	case statusCode == 401 || statusCode == 403:
		return "authentication_error"
	case statusCode == 404:
		return "not_found_error"
	case statusCode == 429:
		return "rate_limit_error"
	case statusCode >= 500:
		return "api_error"
	default:
		return "api_error"
	}
}
