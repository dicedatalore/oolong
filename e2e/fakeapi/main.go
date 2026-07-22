// Command fakeapi is a fake OpenAI, Anthropic, Google Gemini, and Ollama API for
// driving Oolong end-to-end. It serves the providers' model and streaming
// chat shapes.
//
// Environment:
//
//	REQLOG             append each /v1/responses request body to this file
//	ANTHROPIC_REQLOG   append each /v1/messages request body to this file
//	GEMINI_REQLOG      append each streamGenerateContent request body to this file
//	OLLAMA_REQLOG      append each /api/chat request body to this file
//	REPLY_FILE           stream this file's text instead of the built-in chunks
//	OPENAI_REPLY_FILE    override REPLY_FILE for OpenAI responses
//	ANTHROPIC_REPLY_FILE override REPLY_FILE for Anthropic responses
//	GEMINI_REPLY_FILE    override REPLY_FILE for Gemini responses
//	OLLAMA_REPLY_FILE    override REPLY_FILE for Ollama responses
//	REPLY_DELAY_MS       delay between streamed words (default 0; the demo uses
//	                     ~40 to look like a real model thinking)
//
// It listens on the address in os.Args[1] (use 127.0.0.1:0 for a free
// port) and prints "listening on <addr>" once ready, so callers can parse
// the bound address.
package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fakeapi <listen-addr>")
		os.Exit(2)
	}
	http.HandleFunc("/v1/models", models)
	http.HandleFunc("/v1/responses", responses)
	http.HandleFunc("/v1/messages", messages)
	http.HandleFunc("/v1beta/models/", generateContent)
	http.HandleFunc("/api/chat", ollamaChat)

	ln, err := net.Listen("tcp", os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("listening on", ln.Addr())
	http.Serve(ln, nil)
}

func models(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(r.Header.Get("Authorization"), "bad-key") || r.Header.Get("x-api-key") == "bad-key" {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid API key"}}`)
		return
	}
	if r.Header.Get("x-api-key") != "" {
		fmt.Fprint(w, `{"data":[`+
			`{"id":"claude-haiku-4-5","created_at":"2026-01-01T00:00:00Z","display_name":"Claude Haiku 4.5","type":"model"},`+
			`{"id":"claude-sonnet-5","created_at":"2026-01-01T00:00:00Z","display_name":"Claude Sonnet 5","type":"model"}],`+
			`"has_more":false,"first_id":"claude-haiku-4-5","last_id":"claude-sonnet-5"}`)
		return
	}
	fmt.Fprint(w, `{"object":"list","data":[`+
		`{"id":"gpt-5.6-luna","object":"model","created":1,"owned_by":"openai"},`+
		`{"id":"gpt-5.6-terra","object":"model","created":1,"owned_by":"openai"},`+
		`{"id":"gpt-5.6-sol","object":"model","created":1,"owned_by":"openai"}]}`)
}

// ollamaChat fakes Ollama's newline-delimited native streaming response.
func ollamaChat(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	logRequest("OLLAMA_REQLOG", body)
	w.Header().Set("Content-Type", "application/x-ndjson")
	fl := w.(http.Flusher)
	for _, part := range replyChunks("OLLAMA_REPLY_FILE") {
		fmt.Fprintf(w, "{\"message\":{\"role\":\"assistant\",\"content\":%s},\"done\":false}\n", strconv.Quote(part))
		fl.Flush()
		time.Sleep(replyDelay())
	}
	fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":10,"eval_count":30}`)
	fl.Flush()
}

func responses(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	logRequest("REQLOG", body)

	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	delay := time.Duration(0)
	if ms, err := strconv.Atoi(os.Getenv("REPLY_DELAY_MS")); err == nil {
		delay = time.Duration(ms) * time.Millisecond
	}
	for _, chunk := range replyChunks("OPENAI_REPLY_FILE") {
		fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%s}\n\n", strconv.Quote(chunk))
		fl.Flush()
		time.Sleep(delay)
	}
	fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":30,\"total_tokens\":40}}}\n\n")
	fl.Flush()
}

func messages(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	logRequest("ANTHROPIC_REQLOG", body)
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\n")
	fl.Flush()
	for _, chunk := range replyChunks("ANTHROPIC_REPLY_FILE") {
		fmt.Fprintf(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}\n\n", strconv.Quote(chunk))
		fl.Flush()
		time.Sleep(replyDelay())
	}
	fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":30}}\n\n")
	fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	fl.Flush()
}

// generateContent fakes the Gemini API's streamGenerateContent SSE shape.
func generateContent(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	logRequest("GEMINI_REQLOG", body)
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	for _, chunk := range replyChunks("GEMINI_REPLY_FILE") {
		fmt.Fprintf(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":%s}],\"role\":\"model\"}}],\"usageMetadata\":{\"promptTokenCount\":10}}\n\n", strconv.Quote(chunk))
		fl.Flush()
		time.Sleep(replyDelay())
	}
	fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[],\"role\":\"model\"},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":30}}\n\n")
	fl.Flush()
}

func logRequest(env string, body []byte) {
	if path := os.Getenv(env); path != "" {
		if file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			// Trimmed because some SDKs (genai) end the body with a newline,
			// which would add blank log lines and break tail-based asserts.
			fmt.Fprintf(file, "%s\n", bytes.TrimSpace(body))
			file.Close()
		}
	}
}

func replyDelay() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("REPLY_DELAY_MS")); err == nil {
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

// replyChunks returns the reply as streamable deltas. A provider-specific
// file wins over REPLY_FILE; either is split into word-sized chunks with
// whitespace retained so the text reassembles exactly.
func replyChunks(providerEnv string) []string {
	path := os.Getenv(providerEnv)
	if path == "" {
		path = os.Getenv("REPLY_FILE")
	}
	if path == "" {
		return []string{"fake ", "reply ", "done"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"fakeapi: " + err.Error()}
	}
	var chunks []string
	rest := string(data)
	for rest != "" {
		end := strings.IndexByte(rest, ' ')
		if end < 0 {
			chunks = append(chunks, rest)
			break
		}
		chunks = append(chunks, rest[:end+1])
		rest = rest[end+1:]
	}
	return chunks
}
