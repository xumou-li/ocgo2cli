package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteSSEEventMessageStart(t *testing.T) {
	var buf bytes.Buffer
	event := MessageEvent{
		Type: "message_start",
		Message: &MessageResponse{
			ID:      "msg_001",
			Type:    "message",
			Role:    "assistant",
			Model:   "claude-sonnet-4-20250514",
			Content: []ContentBlock{},
		},
	}

	err := writeSSEEvent(&buf, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.HasPrefix(output, "event: message_start\n") {
		t.Errorf("expected event prefix, got: %s", output)
	}
	if !strings.Contains(output, "data: ") {
		t.Errorf("expected data line, got: %s", output)
	}
}

func TestWriteSSEEventDelta(t *testing.T) {
	var buf bytes.Buffer
	idx := 0
	event := MessageEvent{
		Type:  "content_block_delta",
		Index: &idx,
		Delta: &Delta{
			Type: "text_delta",
			Text: "Hello",
		},
	}

	err := writeSSEEvent(&buf, event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "text_delta") {
		t.Errorf("expected text_delta in payload, got: %s", output)
	}
	if !strings.Contains(output, "Hello") {
		t.Errorf("expected Hello in payload, got: %s", output)
	}
}

func TestGenerateID(t *testing.T) {
	id := generateID()
	if !strings.HasPrefix(id, "msg_") {
		t.Errorf("expected ID to start with msg_, got: %s", id)
	}
	if len(id) != 4+18 {
		t.Errorf("expected ID length 22, got %d: %s", len(id), id)
	}

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateID()
		if ids[id] {
			t.Errorf("duplicate ID generated: %s", id)
		}
		ids[id] = true
	}
}

func TestReadSSELine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"data prefix", "data: {\"key\":\"value\"}\n", `{"key":"value"}`},
		{"data with no space", "data:{\"key\":\"value\"}\n", `{"key":"value"}`},
		{"empty line", "\n", ""},
		{"comment", ":heartbeat\n", ""},
		{"other event", "event: message_start\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := bufio.NewReader(strings.NewReader(tt.input))
			got, err := readSSELine(buf)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProxyStreamBasicText(t *testing.T) {
	w := httptest.NewRecorder()

	chunks := []string{
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		``,
		`data: [DONE]`,
		``,
	}

	body := io.NopCloser(strings.NewReader(strings.Join(chunks, "\n")))
	ctx := context.Background()

	err := ProxyStream(w, body, "claude-sonnet-4-20250514", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := w.Body.String()
	if !strings.Contains(output, "message_start") {
		t.Error("expected message_start event")
	}
	if !strings.Contains(output, "text_delta") {
		t.Error("expected text_delta event")
	}
	if !strings.Contains(output, "Hello") {
		t.Error("expected Hello in output")
	}
	if !strings.Contains(output, "world") {
		t.Error("expected world in output")
	}
	if !strings.Contains(output, "message_stop") {
		t.Error("expected message_stop event")
	}
}

func TestProxyStreamWithReasoningContent(t *testing.T) {
	w := httptest.NewRecorder()

	chunks := []string{
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Let me think..."},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"content":"Answer"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		``,
		`data: [DONE]`,
		``,
	}

	body := io.NopCloser(strings.NewReader(strings.Join(chunks, "\n")))
	ctx := context.Background()

	err := ProxyStream(w, body, "claude-sonnet-4-20250514", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := w.Body.String()
	if !strings.Contains(output, "thinking_delta") {
		t.Error("expected thinking_delta event")
	}
	if !strings.Contains(output, "Let me think...") {
		t.Error("expected reasoning content in output")
	}
	if !strings.Contains(output, "content_block_start") {
		t.Error("expected content_block_start events")
	}
	if !strings.Contains(output, "content_block_stop") {
		t.Error("expected content_block_stop events")
	}
}

func TestProxyStreamWithToolCalls(t *testing.T) {
	w := httptest.NewRecorder()

	chunks := []string{
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_001","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"Beijing\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`,
		``,
		`data: [DONE]`,
		``,
	}

	body := io.NopCloser(strings.NewReader(strings.Join(chunks, "\n")))
	ctx := context.Background()

	err := ProxyStream(w, body, "claude-sonnet-4-20250514", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := w.Body.String()
	if !strings.Contains(output, "input_json_delta") {
		t.Error("expected input_json_delta event")
	}
	if !strings.Contains(output, "tool_use") {
		t.Error("expected tool_use in content_block_start")
	}
	if !strings.Contains(output, "get_weather") {
		t.Error("expected get_weather in output")
	}
}

func TestProxyStreamClientDisconnect(t *testing.T) {
	w := httptest.NewRecorder()

	chunks := `data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`

	body := io.NopCloser(strings.NewReader(chunks))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ProxyStream(w, body, "claude-model", ctx)
	if err != ErrClientDisconnected {
		t.Errorf("expected ErrClientDisconnected, got %v", err)
	}
}

func TestProxyStreamDONEHandling(t *testing.T) {
	w := httptest.NewRecorder()

	chunks := []string{
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-001","object":"chat.completion.chunk","created":123,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`,
		``,
		`data: [DONE]`,
		``,
	}

	body := io.NopCloser(strings.NewReader(strings.Join(chunks, "\n")))
	ctx := context.Background()

	err := ProxyStream(w, body, "claude-model", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := w.Body.String()
	if !strings.Contains(output, "message_stop") {
		t.Error("expected message_stop after [DONE]")
	}
}

// Regression: verify JSON encoding of SSE events is valid
func TestSSEEventJSONValidity(t *testing.T) {
	var buf bytes.Buffer

	events := []MessageEvent{
		{Type: "message_start"},
		{Type: "content_block_start", Index: intPtr(0), ContentBlock: &ContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: intPtr(0), Delta: &Delta{Type: "text_delta", Text: "hi"}},
		{Type: "content_block_stop", Index: intPtr(0)},
		{Type: "message_delta", Delta: &Delta{StopReason: "end_turn"}, Usage: &Usage{InputTokens: 10, OutputTokens: 5}},
		{Type: "message_stop"},
	}

	for _, evt := range events {
		buf.Reset()
		err := writeSSEEvent(&buf, evt)
		if err != nil {
			t.Errorf("writeSSEEvent(%s) error: %v", evt.Type, err)
		}
		output := buf.String()
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				jsonData := strings.TrimPrefix(line, "data: ")
				var v interface{}
				if err := json.Unmarshal([]byte(jsonData), &v); err != nil {
					t.Errorf("invalid JSON for event %s: %v\n  data: %s", evt.Type, err, jsonData)
				}
			}
		}
	}
}

func TestStringContent(t *testing.T) {
	if got := stringContent("hello"); got != "hello" {
		t.Errorf("expected hello, got %s", got)
	}
	if got := stringContent([]interface{}{
		map[string]interface{}{"text": "part1"},
		map[string]interface{}{"text": "part2"},
	}); got != "part1part2" {
		t.Errorf("expected part1part2, got %s", got)
	}
	if got := stringContent(nil); got != "" {
		t.Errorf("expected empty for nil, got %s", got)
	}
}

func intPtr(i int) *int { return &i }
