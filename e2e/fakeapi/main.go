// Command fakeapi is a fake OpenAI API for driving Oolong end-to-end:
// GET /v1/models returns a fixed catalog and POST /v1/responses streams an
// SSE reply. It backs the CI smoke test (e2e/smoke.sh) and the demo GIF
// recording (demo/record.sh).
//
// Environment:
//
//	REQLOG         append each /v1/responses request body to this file
//	REPLY_FILE     stream this file's text instead of the built-in chunks
//	REPLY_DELAY_MS delay between streamed words (default 0; the demo uses
//	               ~40 to look like a real model thinking)
//
// It listens on the address in os.Args[1] (use 127.0.0.1:0 for a free
// port) and prints "listening on <addr>" once ready, so callers can parse
// the bound address.
package main

import (
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
	fmt.Fprint(w, `{"object":"list","data":[`+
		`{"id":"gpt-5.6-luna","object":"model","created":1,"owned_by":"openai"},`+
		`{"id":"gpt-5.6-terra","object":"model","created":1,"owned_by":"openai"},`+
		`{"id":"gpt-5.6-sol","object":"model","created":1,"owned_by":"openai"}]}`)
}

func responses(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if reqlog := os.Getenv("REQLOG"); reqlog != "" {
		if f, err := os.OpenFile(reqlog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "%s\n", body)
			f.Close()
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	delay := time.Duration(0)
	if ms, err := strconv.Atoi(os.Getenv("REPLY_DELAY_MS")); err == nil {
		delay = time.Duration(ms) * time.Millisecond
	}
	for _, chunk := range replyChunks() {
		fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%s}\n\n", strconv.Quote(chunk))
		fl.Flush()
		time.Sleep(delay)
	}
	fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":30,\"total_tokens\":40}}}\n\n")
	fl.Flush()
}

// replyChunks returns the reply as streamable deltas: REPLY_FILE split into
// word-sized chunks (whitespace kept, so the text reassembles exactly), or
// the built-in canned chunks.
func replyChunks() []string {
	path := os.Getenv("REPLY_FILE")
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
