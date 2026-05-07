package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/kardianos/service"
)

// Version is the current version of ocgo2cli.
const Version = "1.0.0"

var cfg *Config

// program implements service.Interface for daemon management.
type program struct {
	srv           *http.Server
	serverStarted chan error // receives result of ListenAndServe
}

func (p *program) Start(s service.Service) error {
	// Start should not block. Run server in a goroutine.
	log.Printf("ocgo2cli daemon starting on %s", cfg.Listen)
	p.serverStarted = make(chan error, 1)
	go p.runServer()
	// Wait briefly for the server to start or fail.
	select {
	case err := <-p.serverStarted:
		if err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func (p *program) runServer() {
	p.srv = &http.Server{
		Addr:    cfg.Listen,
		Handler: newServeMux(),
	}

	log.Printf("ocgo2cli listening on %s", cfg.Listen)
	// Signal that the server has started (non-blocking).
	select {
	case p.serverStarted <- nil:
	default:
	}
	if err := p.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("server error: %v", err)
		select {
		case p.serverStarted <- err:
		default:
		}
	}
}

func (p *program) Stop(s service.Service) error {
	log.Println("ocgo2cli shutting down...")
	if p.srv != nil {
		return p.srv.Shutdown(context.Background())
	}
	return nil
}

// newServeMux creates a configured ServeMux with all routes registered.
func newServeMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", handleMessages)
	mux.HandleFunc("/health", handleHealth)
	return mux
}

// runForeground starts the HTTP server in the foreground (for debug/manual use).
func runForeground() {
	addr := cfg.Listen
	log.Printf("ocgo2cli starting on %s (foreground)", addr)
	if err := http.ListenAndServe(addr, newServeMux()); err != nil {
		log.Fatalf("server: %v", err)
	}
}

// loadConfigOrDie loads configuration, calling log.Fatalf on failure.
func loadConfigOrDie(configPath string) {
	var err error
	cfg, err = LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}
}

func main() {
	// Define our flags before the service framework takes over.
	configPath := flag.String("c", "", "Config file path (default: ~/.config/ocgo2cli/config.json)")
	flag.StringVar(configPath, "config", "", "Config file path (default: ~/.config/ocgo2cli/config.json)")

	// Parse early so we can check for "version"/"run" before service framework.
	// flag.Parse() stops at the first non-flag argument, leaving the command
	// in flag.Args() for inspection.
	flag.Parse()

	// Resolve config path.
	resolvedPath := *configPath
	if resolvedPath == "" {
		var err error
		resolvedPath, err = DefaultConfigPath()
		if err != nil {
			log.Fatalf("config path: %v", err)
		}
	}

	// Ensure default config exists.
	if err := SaveDefaultConfig(resolvedPath); err != nil {
		log.Printf("Warning: could not save default config: %v", err)
	}

	// Load config globally.
	loadConfigOrDie(resolvedPath)

	// Determine the command (first non-flag arg).
	args := flag.Args()
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}

	// Handle commands that don't need the service framework.
	switch cmd {
	case "version":
		fmt.Printf("ocgo2cli version %s\n", Version)
		os.Exit(0)
	case "run":
		runForeground()
		return
	}

	// Build the service configuration.
	svcConfig := &service.Config{
		Name:        "ocgo2cli",
		DisplayName: "ocgo2cli",
		Description: "OpenCode Go to Claude CLI proxy",
		Arguments:   []string{"-c", resolvedPath},
		Option: service.KeyValue{
			"UserService": true, // install at user level, no sudo required
		},
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatalf("create service: %v", err)
	}

	// Handle service control commands.
	if cmd != "" {
		err = service.Control(s, cmd)
		if err != nil {
			log.Fatalf("%s: %v", cmd, err)
		}
		if cmd == "install" {
			fmt.Printf("Service installed (user level). Config: %s\nRun 'ocgo2cli start' to begin.\n", resolvedPath)
		}
		return
	}

	// No command given → run as a daemon (called by service manager).
	log.Printf("ocgo2cli daemon starting (config: %s)", resolvedPath)
	err = s.Run()
	if err != nil {
		log.Fatalf("run service: %v", err)
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
