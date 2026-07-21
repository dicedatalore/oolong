package ui

// Shared helpers for the tests in this package. Tests drive the UI the same
// way the Bubble Tea runtime does: build a Model, feed messages to Update,
// and assert on the returned state.

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/openai/openai-go/v3/option"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/provider/openai"
)

func TestMain(m *testing.M) {
	value, hadValue := os.LookupEnv("NO_COLOR")
	_ = os.Unsetenv("NO_COLOR")
	code := m.Run()
	if hadValue {
		_ = os.Setenv("NO_COLOR", value)
	}
	os.Exit(code)
}

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

func TestCenteredBarCentersEveryLine(t *testing.T) {
	lines := strings.Split(centeredBar(20, "short\nlonger line"), "\n")
	for _, line := range lines {
		left := len(line) - len(strings.TrimLeft(line, " "))
		right := len(line) - len(strings.TrimRight(line, " "))
		if diff := left - right; diff < -1 || diff > 1 {
			t.Errorf("line is not centered: %q (%d left, %d right)", line, left, right)
		}
	}
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

func TestReducedMotionDisablesDecorativeTicks(t *testing.T) {
	m := New(nil, "dark", config.Config{ReducedMotion: true}, "")
	if m.Init() != nil {
		t.Error("reduced motion scheduled the picker animation")
	}
	if got := m.activityIndicator(); got != "•" {
		t.Errorf("reduced-motion activity indicator = %q", got)
	}
	if _, cmd := m.handleSparkle(sparkleMsg{tag: m.sparkleTag}); cmd != nil {
		t.Error("reduced motion rescheduled a sparkle tick")
	}
}

func TestNoColorDisablesThemeColorsAndGradient(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New(nil, "dark", config.Config{}, "")
	if !m.theme.noColor {
		t.Fatal("NO_COLOR was not applied")
	}
	if got := gradientRow(logoRows[0], m.theme); got != logoRows[0] {
		t.Errorf("no-color logo row = %q", got)
	}
}

func TestChatHeaderProgressivelyCompacts(t *testing.T) {
	m := New(nil, "dark", config.Config{}, "")
	m.chosen = "a-very-long-model-name-for-testing"
	m.inputTokens, m.outputTokens = 1200, 300
	for _, tt := range []struct {
		width int
		want  string
		avoid string
	}{
		{100, "in /", ""},
		{70, "in /", "$"},
		{50, "tokens", "$"},
		{28, "a-very", "tokens"},
	} {
		m.width = tt.width
		plain := ansi.ReplaceAllString(m.chatHeader(), "")
		if !strings.Contains(plain, tt.want) || (tt.avoid != "" && strings.Contains(plain, tt.avoid)) {
			t.Errorf("width %d header = %q, want %q and avoid %q", tt.width, plain, tt.want, tt.avoid)
		}
		if lipgloss.Width(plain) > tt.width {
			t.Errorf("width %d header rendered %d columns", tt.width, lipgloss.Width(plain))
		}
	}
}
