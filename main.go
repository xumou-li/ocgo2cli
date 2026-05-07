package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

var cfg *Config

func main() {
	configPath, err := DefaultConfigPath()
	if err != nil {
		log.Fatalf("config path: %v", err)
	}

	if err := SaveDefaultConfig(configPath); err != nil {
		log.Printf("Warning: could not save default config: %v", err)
	}

	cfg, err = LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Printf("Warning: config validation failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", handleMessages)
	mux.HandleFunc("/health", handleHealth)

	addr := cfg.Listen
	log.Printf("ocgo2cli starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAnthropicError(w, http.StatusMethodNotAllowed, "only POST is supported")
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse Anthropic request
	var anthropicReq MessageRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicError(w, http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
		return
	}

	// Lookup model config
	modelConfig, ok := cfg.Models[anthropicReq.Model]
	if !ok {
		writeAnthropicError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown model: %s", anthropicReq.Model))
		return
	}

	isStream := anthropicReq.Stream != nil && *anthropicReq.Stream

	// Branch: Anthropic-native vs OpenAI
	if IsAnthropicModel(modelConfig.ModelID) {
		handleAnthropicNative(w, r, body, modelConfig, isStream, anthropicReq.Model)
		return
	}

	handleOpenAIModel(w, r, &anthropicReq, modelConfig, isStream)
}

func handleAnthropicNative(w http.ResponseWriter, r *http.Request, rawBody []byte, modelConfig ModelConfig, isStream bool, originalModel string) {
	// Replace model name in raw JSON body
	modifiedBody := bytes.Replace(rawBody,
		[]byte(`"model":"`+originalModel+`"`),
		[]byte(`"model":"`+modelConfig.ModelID+`"`),
		1)

	// Also handle with spaces like "model": "name"
	modifiedBody = bytes.Replace(modifiedBody,
		[]byte(`"model": "`+originalModel+`"`),
		[]byte(`"model": "`+modelConfig.ModelID+`"`),
		1)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		cfg.OpenCodeAnthropicBaseURL, bytes.NewReader(modifiedBody))
	if err != nil {
		writeAnthropicError(w, http.StatusInternalServerError, "failed to create upstream request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+resolveEnv(cfg.APIKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		writeAnthropicError(w, resp.StatusCode, string(body))
		return
	}

	// For Anthropic-native models, response is already in Anthropic format.
	// Just replace the model name and proxy through.
	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Stream the response, replacing model name in SSE events
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				// Replace model name in the chunk
				chunk := buf[:n]
				chunk = bytes.Replace(chunk,
					[]byte(`"model":"`+modelConfig.ModelID+`"`),
					[]byte(`"model":"`+originalModel+`"`),
					-1)
				chunk = bytes.Replace(chunk,
					[]byte(`"model": "`+modelConfig.ModelID+`"`),
					[]byte(`"model": "`+originalModel+`"`),
					-1)
				w.Write(chunk)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			if err != nil {
				break
			}
		}
	} else {
		// Non-streaming: read full response, replace model name, return
		respBody, _ := io.ReadAll(resp.Body)
		respBody = bytes.Replace(respBody,
			[]byte(`"model":"`+modelConfig.ModelID+`"`),
			[]byte(`"model":"`+originalModel+`"`),
			-1)
		respBody = bytes.Replace(respBody,
			[]byte(`"model": "`+modelConfig.ModelID+`"`),
			[]byte(`"model": "`+originalModel+`"`),
			-1)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
	}
}

func handleOpenAIModel(w http.ResponseWriter, r *http.Request, anthropicReq *MessageRequest, modelConfig ModelConfig, isStream bool) {
	openaiReq, err := TransformRequest(anthropicReq, modelConfig)
	if err != nil {
		writeAnthropicError(w, http.StatusBadRequest, fmt.Sprintf("transform request: %v", err))
		return
	}

	body, err := json.Marshal(openaiReq)
	if err != nil {
		writeAnthropicError(w, http.StatusInternalServerError, "marshal request")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		cfg.OpenCodeBaseURL, bytes.NewReader(body))
	if err != nil {
		writeAnthropicError(w, http.StatusInternalServerError, "create upstream request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+resolveEnv(cfg.APIKey))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		writeAnthropicError(w, resp.StatusCode, string(errBody))
		return
	}

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		if err := ProxyStream(w, resp.Body, anthropicReq.Model, r.Context()); err != nil {
			if err != ErrClientDisconnected {
				log.Printf("stream error: %v", err)
			}
		}
	} else {
		var openaiResp ChatCompletionResponse
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			writeAnthropicError(w, http.StatusBadGateway, "read upstream response")
			return
		}

		if err := json.Unmarshal(respBody, &openaiResp); err != nil {
			writeAnthropicError(w, http.StatusBadGateway, fmt.Sprintf("parse upstream response: %v", err))
			return
		}

		anthropicResp, err := TransformResponse(&openaiResp, anthropicReq.Model)
		if err != nil {
			writeAnthropicError(w, http.StatusInternalServerError, fmt.Sprintf("transform response: %v", err))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(anthropicResp)
	}
}

func writeAnthropicError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	// Strip upstream prefix noise for cleaner error messages
	if strings.HasPrefix(message, "upstream error: ") {
		message = strings.TrimPrefix(message, "upstream error: ")
	}
	json.NewEncoder(w).Encode(TransformErrorResponse(statusCode, message))
}
