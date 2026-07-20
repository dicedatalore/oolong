package ui

// Tests for config-driven behavior: custom catalogs, default_model skipping
// the picker, per-model routes, and reasoning effort controls.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/zalando/go-keyring"

	"github.com/dicedatalore/oolong/internal/config"
)

func customCatalog() []config.Model {
	return []config.Model{
		{ID: "gpt-5.4", Description: "Previous generation", InputRate: 1.25, OutputRate: 10},
		{ID: "gpt-5.6-terra", Description: "Balances intelligence and cost", InputRate: 2.50, OutputRate: 15},
	}
}

// newCustomModel builds a sized model with isolated credentials.
func newCustomModel(t *testing.T, srv *httptest.Server, cfg config.Config) tea.Model {
	t.Helper()
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	var model tea.Model = New(clientFor(srv), "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return model
}

func TestCustomCatalogAppearsImmediately(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{Models: customCatalog()})
	am := model.(Model)
	if len(am.rates) != 2 {
		t.Errorf("rates = %v, want both custom models", am.rates)
	}
	if n := len(pickerModels(am)); n != 2 {
		t.Fatalf("picker shows %d models, want 2", n)
	}
	item := pickerModels(am)[0]
	if item.id != "gpt-5.4" || !strings.Contains(item.desc, "$1.25 in / $10 out") {
		t.Errorf("first item = %+v, want gpt-5.4 with rates", item)
	}
}

func TestStreamUsesPerModelEndpoint(t *testing.T) {
	// Resolve must find the env key so no test ever touches the OS keychain.
	t.Setenv("OPENAI_API_KEY", "sk-test")
	var globalResponses int
	global := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			globalResponses++
		}
	}))
	defer global.Close()
	var localResponses int
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/responses") {
			localResponses++
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer local.Close()

	cfg := config.Config{
		DefaultModel: "local-llama",
		Models:       []config.Model{{ID: "local-llama", BaseURL: local.URL}},
	}
	model := newCustomModel(t, global, cfg)
	if model.(Model).state != stateChat {
		t.Fatal("default_model did not open the chat")
	}
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pumpStream(t, model)
	if localResponses != 1 || globalResponses != 0 {
		t.Errorf("responses hit local %d / global %d times, want 1 / 0",
			localResponses, globalResponses)
	}
}

func TestDefaultModelSkipsPicker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{DefaultModel: "gpt-5.6-terra"})
	am := model.(Model)
	if am.state != stateChat {
		t.Fatalf("state = %v, want stateChat (default_model skips the picker)", am.state)
	}
	if am.chosen != "gpt-5.6-terra" {
		t.Errorf("chosen = %q, want gpt-5.6-terra", am.chosen)
	}
}

func TestNoDefaultModelShowsPicker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{})
	if got := model.(Model).state; got != statePicker {
		t.Fatalf("state = %v, want statePicker", got)
	}
}

func TestConfigErrorSurfacesOnPicker(t *testing.T) {
	var model tea.Model = New(nil, "dark", config.Config{}, "config: something broke")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	am := model.(Model)
	am.state = statePicker // nil client starts on key entry; the notice belongs to the picker
	if v := am.viewPicker(); !strings.Contains(v, "config: something broke") {
		t.Error("picker view does not show the config error")
	}
}

func TestAccentIsOwnedByEachModel(t *testing.T) {
	custom := New(nil, "dark", config.Config{Accent: "#112233"}, "")
	defaults := New(nil, "dark", config.Config{}, "")
	customView := custom.theme.notice.Render("notice")
	defaultView := defaults.theme.notice.Render("notice")
	if customView == defaultView {
		t.Fatal("custom and default themes render the same accent")
	}
	if got := New(nil, "dark", config.Config{}, "").theme.notice.Render("notice"); got != defaultView {
		t.Fatal("constructing a custom theme mutated later default models")
	}
}

func TestPickerEffortAdjust(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	var model tea.Model = New(clientFor(srv), "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	selectedTitle := func() string {
		return model.(Model).picker.SelectedItem().(modelItem).Title()
	}
	if got := selectedTitle(); got != "gpt-5.6-luna" {
		t.Fatalf("initial title = %q, want a bare model id", got)
	}

	// Right steps up the ladder from the model default, clamping at xhigh.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := selectedTitle(); got != "gpt-5.6-luna • effort: none" {
		t.Fatalf("title after one right = %q, want effort: none", got)
	}
	for range 6 {
		model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if got := selectedTitle(); got != "gpt-5.6-luna • effort: xhigh" {
		t.Fatalf("title after many rights = %q, want clamped at xhigh", got)
	}

	// Left steps back down, clamping at the model default.
	for range 7 {
		model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	}
	if got := selectedTitle(); got != "gpt-5.6-luna" {
		t.Fatalf("title after many lefts = %q, want the bare id again", got)
	}

	// The chosen effort applies to the chat and shows in its header; the
	// effort keys only touch the selected model.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // none → low
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := selectedTitle(); got != "gpt-5.6-terra" {
		t.Fatalf("second model title = %q, want untouched", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	if am.state != stateChat {
		t.Fatal("enter did not open the chat")
	}
	if v := am.viewChat(); !strings.Contains(v, "effort: low") {
		t.Error("chat header does not show the picker-chosen effort")
	}
}

func TestStreamCarriesEffortFromConfigAndPicker(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer srv.Close()

	cfg := config.Config{
		DefaultModel: "gpt-5.4",
		Models: []config.Model{
			{ID: "gpt-5.4", ReasoningEffort: "medium", Verbosity: "low"},
		},
	}
	model := newCustomModel(t, srv, cfg)
	if model.(Model).state != stateChat {
		t.Fatal("default_model did not open the chat")
	}

	// The model's configured default applies untouched.
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)
	for _, want := range []string{`"effort":"medium"`, `"verbosity":"low"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request missing %s:\n%s", want, body)
		}
	}

	// Back on the picker, one right (medium → high) retunes the model; the
	// continued chat picks the new effort up.
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if model.(Model).state != statePicker {
		t.Fatal("esc did not return to the picker")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = typeText(model, "again")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pumpStream(t, model)
	if !strings.Contains(string(body), `"effort":"high"`) {
		t.Errorf("request did not carry the picker-adjusted effort:\n%s", body)
	}
}
