package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// streamState tracks the current streaming state for SSE conversion.
type streamState struct {
	w             io.Writer
	flusher       http.Flusher
	originalModel string
	msgID         string // shared message ID for all events

	blockIndex  int      // next content block index to assign
	openBlocks  []string // types of currently open blocks
	toolIndices map[int]int // OpenAI tool_call index → Anthropic content block index

	hasStarted  bool // whether message_start has been sent
	currentType string // current delta type being emitted ("thinking", "text", "tool_use")
}

// ProxyStream reads OpenAI SSE chunks from openaiResp, transforms them to
// Anthropic SSE events, and writes them to w. It respects clientCtx for
// disconnect detection.
func ProxyStream(w http.ResponseWriter, openaiResp io.ReadCloser, originalModel string, clientCtx context.Context) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	s := &streamState{
		w:             w,
		flusher:       flusher,
		originalModel: originalModel,
		msgID:         generateID(),
		toolIndices:   make(map[int]int),
	}

	// Start heartbeat goroutine
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go s.heartbeat(heartbeatDone, clientCtx)

	reader := bufio.NewReader(openaiResp)

	for {
		// Check for client disconnect
		select {
		case <-clientCtx.Done():
			return ErrClientDisconnected
		default:
		}

		line, err := readSSELine(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read SSE: %w", err)
		}

		if line == "" {
			continue
		}

		// Handle [DONE] marker
		if line == "[DONE]" {
			s.closeAllBlocks()
			s.writeEvent(MessageEvent{
				Type: "message_stop",
			})
			s.flush()
			return nil
		}

		// Parse OpenAI chunk
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		if err := s.handleChunk(&chunk); err != nil {
			return fmt.Errorf("handle chunk: %w", err)
		}
	}
}

// ErrClientDisconnected is returned when the client disconnects mid-stream.
var ErrClientDisconnected = fmt.Errorf("client disconnected")

func (s *streamState) handleChunk(chunk *ChatCompletionChunk) error {
	// Send message_start on first chunk
	if !s.hasStarted {
		s.writeEvent(MessageEvent{
			Type: "message_start",
			Message: &MessageResponse{
				ID:      s.msgID,
				Type:    "message",
				Role:    "assistant",
				Model:   s.originalModel,
				Content: []ContentBlock{},
			},
		})
		s.hasStarted = true
	}

	if len(chunk.Choices) == 0 {
		// Usage-only chunk (no delta)
		if chunk.Usage != nil {
			s.writeEvent(MessageEvent{
				Type: "message_delta",
				Delta: &Delta{
					StopReason: "end_turn",
				},
				Usage: &Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				},
			})
		}
		return nil
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	// Handle reasoning_content delta
	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		if err := s.emitThinkingDelta(*delta.ReasoningContent); err != nil {
			return err
		}
	}

	// Handle content delta
	if delta.Content != nil {
		contentStr := stringContent(delta.Content)
		if contentStr != "" {
			if err := s.emitTextDelta(contentStr); err != nil {
				return err
			}
		}
	}

	// Handle tool_calls delta
	for _, tc := range delta.ToolCalls {
		if err := s.emitToolCallDelta(tc); err != nil {
			return err
		}
	}

	// Handle finish_reason
	if choice.FinishReason != "" {
		// Close all open blocks
		s.closeAllBlocks()

		stopReason := mapFinishReason(choice.FinishReason)

		// Send message_delta with stop_reason and usage
		evt := MessageEvent{
			Type: "message_delta",
			Delta: &Delta{
				StopReason: stopReason,
			},
		}
		if chunk.Usage != nil {
			evt.Usage = &Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
		s.writeEvent(evt)

		// Send message_stop
		s.writeEvent(MessageEvent{
			Type: "message_stop",
		})
	}

	s.flush()
	return nil
}

func (s *streamState) emitThinkingDelta(text string) error {
	// If currently in text block, close it first
	if s.currentType == "text" {
		s.closeCurrentBlock()
	}

	// Start thinking block if not already in one
	if s.currentType != "thinking" {
		idx := s.blockIndex
		s.blockIndex++
		s.writeEvent(MessageEvent{
			Type: "content_block_start",
			Index: &idx,
			ContentBlock: &ContentBlock{
				Type:     "thinking",
				Thinking: "",
			},
		})
		s.openBlocks = append(s.openBlocks, "thinking")
		s.currentType = "thinking"
	}

	blockIdx := len(s.openBlocks) - 1
	s.writeEvent(MessageEvent{
		Type: "content_block_delta",
		Index: &blockIdx,
		Delta: &Delta{
			Type:     "thinking_delta",
			Thinking: text,
		},
	})
	return nil
}

func (s *streamState) emitTextDelta(text string) error {
	// Close thinking block if open
	if s.currentType == "thinking" {
		s.closeCurrentBlock()
	}

	// Start text block if not already in one
	if s.currentType != "text" {
		idx := s.blockIndex
		s.blockIndex++
		s.writeEvent(MessageEvent{
			Type: "content_block_start",
			Index: &idx,
			ContentBlock: &ContentBlock{
				Type: "text",
				Text: "",
			},
		})
		s.openBlocks = append(s.openBlocks, "text")
		s.currentType = "text"
	}

	blockIdx := len(s.openBlocks) - 1
	s.writeEvent(MessageEvent{
		Type: "content_block_delta",
		Index: &blockIdx,
		Delta: &Delta{
			Type: "text_delta",
			Text: text,
		},
	})
	return nil
}

func (s *streamState) emitToolCallDelta(tc ToolCall) error {
	// Close any non-tool-call block
	if s.currentType != "tool_use" && s.currentType != "" {
		if s.currentType == "thinking" || s.currentType == "text" {
			s.closeCurrentBlock()
		}
	}

	// Map OpenAI tool call index to Anthropic content block index
	anthIdx, exists := s.toolIndices[tc.Index]
	if !exists {
		// New tool call — start a new tool_use block
		anthIdx = s.blockIndex
		s.blockIndex++
		s.toolIndices[tc.Index] = anthIdx

		s.writeEvent(MessageEvent{
			Type: "content_block_start",
			Index: &anthIdx,
			ContentBlock: &ContentBlock{
				Type: "tool_use",
				ID:   tc.ID,
				Name: tc.Function.Name,
			},
		})
		s.openBlocks = append(s.openBlocks, "tool_use")
		s.currentType = "tool_use"
	}

	// Emit input_json_delta
	if tc.Function.Arguments != "" {
		s.writeEvent(MessageEvent{
			Type: "content_block_delta",
			Index: &anthIdx,
			Delta: &Delta{
				Type:        "input_json_delta",
				PartialJSON: tc.Function.Arguments,
			},
		})
	}
	return nil
}

func (s *streamState) closeCurrentBlock() {
	if len(s.openBlocks) == 0 {
		return
	}
	idx := len(s.openBlocks) - 1
	s.writeEvent(MessageEvent{
		Type: "content_block_stop",
		Index: &idx,
	})
	s.openBlocks = s.openBlocks[:len(s.openBlocks)-1]
	s.currentType = ""
	if len(s.openBlocks) > 0 {
		s.currentType = s.openBlocks[len(s.openBlocks)-1]
	}
}

func (s *streamState) closeAllBlocks() {
	for len(s.openBlocks) > 0 {
		s.closeCurrentBlock()
	}
}

func (s *streamState) heartbeat(done <-chan struct{}, ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			fmt.Fprintf(s.w, ":keepalive\n\n")
			s.flush()
		}
	}
}

// readSSELine reads a line from the SSE stream, stripping the "data: " prefix.
func readSSELine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", nil
	}
	if strings.HasPrefix(line, "data: ") {
		return line[6:], nil
	}
	if strings.HasPrefix(line, "data:") {
		return line[5:], nil
	}
	// Ignore other SSE fields (event:, id:, retry:, comments)
	if strings.HasPrefix(line, ":") {
		return "", nil
	}
	return "", nil
}

// writeSSEEvent writes a single Anthropic SSE event.
func writeSSEEvent(w io.Writer, event interface{}) error {
	var evtType string

	switch e := event.(type) {
	case MessageEvent:
		evtType = e.Type
	default:
		evtType = "message_stop"
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal SSE event: %w", err)
	}

	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evtType, data)
	return err
}

func (s *streamState) writeEvent(event interface{}) {
	writeSSEEvent(s.w, event)
}

func (s *streamState) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func generateID() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 18)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := range b {
		b[i] = charset[r.Intn(len(charset))]
	}
	return "msg_" + string(b)
}

// stringContent extracts string content from interface{} that could be string or array.
func stringContent(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		var parts []string
		for _, part := range val {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := partMap["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}
