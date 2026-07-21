package ui

// Shared helpers for the tests in this package. Tests drive the UI the same
// way the Bubble Tea runtime does: build a Model, feed messages to Update,
// and assert on the returned state.

import (
	"net/http/httptest"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/openai/openai-go/v3/option"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/provider/openai"
)

// clientFor returns a client that talks to the fake server instead of the
// real API.
func clientFor(srv *httptest.Server) *openai.Client {
	return openai.New("test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
}

// enterChat gets a fresh model into the chat state with the first model picked.
func enterChat(t *testing.T, srv *httptest.Server) tea.Model {
	t.Helper()
	var model tea.Model = New(clientFor(srv), "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if model.(Model).state != stateChat {
		t.Fatal("not in chat state after picking a model")
	}
	return model
}

// typeText feeds s to the model one key press at a time.
func typeText(model tea.Model, s string) tea.Model {
	for _, r := range s {
		model, _ = model.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return model
}

func TestSpinnerFadeMovesBetweenBothAccents(t *testing.T) {
	tests := []struct {
		step int
		want float64
	}{
		{step: 0, want: 0},
		{step: 8, want: 0.5},
		{step: 16, want: 1},
		{step: 24, want: 0.5},
		{step: 32, want: 0},
	}
	for _, tt := range tests {
		if got := spinnerFadePosition(tt.step); got != tt.want {
			t.Errorf("spinnerFadePosition(%d) = %v, want %v", tt.step, got, tt.want)
		}
	}
}
