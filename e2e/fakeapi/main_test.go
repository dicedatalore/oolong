package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	provideranthropic "github.com/dicedatalore/oolong/internal/anthropic"
	"github.com/dicedatalore/oolong/internal/chat"
	providergoogle "github.com/dicedatalore/oolong/internal/google"
)

func TestGeminiFakeMatchesSDK(t *testing.T) {
	t.Setenv("REPLY_FILE", "")
	t.Setenv("REPLY_DELAY_MS", "0")
	logPath := t.TempDir() + "/gemini.log"
	t.Setenv("GEMINI_REQLOG", logPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1beta/models/", generateContent)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := providergoogle.New("AIza-test", providergoogle.WithBaseURL(server.URL))
	stream := make(chan chat.StreamEvent)
	go client.StreamChat(context.Background(), "gemini-3.5-flash",
		[]chat.Message{{Role: "user", Content: "hello fake"}}, chat.Options{}, stream)
	var text string
	var usage chat.Usage
	for event := range stream {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		text += event.Delta
		if event.Done {
			usage = event.Usage
		}
	}
	if text != "fake reply done" {
		t.Errorf("reply = %q", text)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 30 {
		t.Errorf("usage = %+v", usage)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "hello fake") {
		t.Errorf("request log = %s", logged)
	}
}

func TestAnthropicFakeMatchesSDK(t *testing.T) {
	t.Setenv("REPLY_FILE", "")
	t.Setenv("REPLY_DELAY_MS", "0")
	logPath := t.TempDir() + "/anthropic.log"
	t.Setenv("ANTHROPIC_REQLOG", logPath)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/messages", messages)
	mux.HandleFunc("/v1/models", models)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := provideranthropic.New("sk-ant-test", provideranthropic.WithBaseURL(server.URL))
	stream := make(chan chat.StreamEvent)
	go client.StreamChat(context.Background(), "claude-sonnet-5",
		[]chat.Message{{Role: "user", Content: "hello fake"}}, chat.Options{}, stream)
	var text string
	var usage chat.Usage
	for event := range stream {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		text += event.Delta
		if event.Done {
			usage = event.Usage
		}
	}
	if text != "fake reply done" {
		t.Errorf("reply = %q", text)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 30 {
		t.Errorf("usage = %+v", usage)
	}
	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), `"model":"claude-sonnet-5"`) ||
		!strings.Contains(string(logged), "hello fake") {
		t.Errorf("request log = %s", logged)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/models", nil)
	request.Header.Set("x-api-key", "sk-ant-test")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "claude-sonnet-5") {
		t.Errorf("Anthropic model list = %s", body)
	}
}
