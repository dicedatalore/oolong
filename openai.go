package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	chatCompletionsURL = "https://api.openai.com/v1/chat/completions"
	modelsURL          = "https://api.openai.com/v1/models"
)

type openaiClient struct {
	apiKey string
	http   *http.Client
}

func newOpenAIClient(apiKey string) *openaiClient {
	return &openaiClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 2 * time.Minute},
	}
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string       `json:"model"`
	Messages []apiMessage `json:"messages"`
}

type apiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message apiMessage `json:"message"`
	} `json:"choices"`
	Usage apiUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *openaiClient) complete(model string, messages []apiMessage) (string, apiUsage, error) {
	var usage apiUsage

	body, err := json.Marshal(chatRequest{Model: model, Messages: messages})
	if err != nil {
		return "", usage, err
	}

	req, err := http.NewRequest(http.MethodPost, chatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		return "", usage, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", usage, err
	}
	defer resp.Body.Close()

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", usage, fmt.Errorf("decoding response (HTTP %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return "", usage, fmt.Errorf("openai: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", usage, fmt.Errorf("openai: empty response (HTTP %d)", resp.StatusCode)
	}
	return parsed.Choices[0].Message.Content, parsed.Usage, nil
}

// validateAPIKey checks the key against the models endpoint, which is free
// and spends no tokens. The API's own 401 message can echo the key back, so
// it is replaced with a generic error.
func validateAPIKey(key string) error {
	req, err := http.NewRequest(http.MethodGet, modelsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("invalid API key")
	default:
		return fmt.Errorf("openai: HTTP %d", resp.StatusCode)
	}
}
