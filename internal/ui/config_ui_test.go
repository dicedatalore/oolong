package ui

// Tests for config-driven behavior: the custom model catalog and its
// availability check, default_model skipping the picker, and the reasoning
// effort controls.

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/dicedatalore/oolong/internal/config"
)

func customCatalog() []config.Model {
	return []config.Model{
		{ID: "gpt-5.4", Description: "Previous generation", InputRate: 1.25, OutputRate: 10},
		{ID: "gpt-5.6-terra", Description: "Balances intelligence and cost", InputRate: 2.50, OutputRate: 15},
	}
}

// newCustomModel builds a sized model with a custom catalog whose
// availability check is still outstanding.
func newCustomModel(t *testing.T, srv *httptest.Server, cfg config.Config) tea.Model {
	t.Helper()
	var model tea.Model = New(clientFor(srv), "dark", cfg, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return model
}

func TestCustomCatalogWaitsForAvailabilityCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{Models: customCatalog()})
	am := model.(Model)
	if n := len(am.picker.Items()); n != 0 {
		t.Errorf("picker shows %d items before the availability check", n)
	}
	if !strings.Contains(am.keyNotice, "checking") {
		t.Errorf("keyNotice = %q, want a checking notice", am.keyNotice)
	}
	// The catalog itself is live already: rates and reasoning defaults must
	// not wait for the check (default_model may already be chatting).
	if len(am.rates) != 2 {
		t.Errorf("rates = %v, want both custom models", am.rates)
	}

	model, _ = model.Update(modelsCheckMsg{available: map[string]bool{"gpt-5.4": true, "gpt-5.6-terra": true}})
	am = model.(Model)
	if n := len(am.picker.Items()); n != 2 {
		t.Fatalf("picker shows %d items after the check, want 2", n)
	}
	if am.keyNotice != "" {
		t.Errorf("keyNotice = %q, want empty after a clean check", am.keyNotice)
	}
	item := am.picker.Items()[0].(modelItem)
	if item.id != "gpt-5.4" || !strings.Contains(item.desc, "$1.25 in / $10 out") {
		t.Errorf("first item = %+v, want gpt-5.4 with rates", item)
	}
}

func TestAvailabilityCheckDropsUnknownModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{Models: customCatalog()})
	model, _ = model.Update(modelsCheckMsg{available: map[string]bool{"gpt-5.6-terra": true}})
	am := model.(Model)
	if n := len(am.picker.Items()); n != 1 {
		t.Fatalf("picker shows %d items, want 1", n)
	}
	if am.picker.Items()[0].(modelItem).id != "gpt-5.6-terra" {
		t.Error("wrong model survived the availability check")
	}
	if !strings.Contains(am.keyNotice, "gpt-5.4") {
		t.Errorf("keyNotice = %q, want it to name the hidden model", am.keyNotice)
	}
}

func TestAvailabilityCheckFallsBackWhenNothingAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{Models: customCatalog()})
	model, _ = model.Update(modelsCheckMsg{available: map[string]bool{}})
	am := model.(Model)
	if n := len(am.picker.Items()); n != len(config.Builtin) {
		t.Fatalf("picker shows %d items, want the %d built-ins", n, len(config.Builtin))
	}
	if !strings.Contains(am.keyNotice, "built-in") {
		t.Errorf("keyNotice = %q, want a fallback notice", am.keyNotice)
	}
}

func TestAvailabilityCheckErrorShowsWholeCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := newCustomModel(t, srv, config.Config{Models: customCatalog()})
	model, _ = model.Update(modelsCheckMsg{err: errors.New("timeout")})
	am := model.(Model)
	if n := len(am.picker.Items()); n != 2 {
		t.Fatalf("picker shows %d items after a failed check, want all 2", n)
	}
	if !strings.Contains(am.keyNotice, "couldn't verify") {
		t.Errorf("keyNotice = %q, want a verification warning", am.keyNotice)
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

func TestEffortCycleAndHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	model := enterChat(t, srv)
	if v := model.(Model).viewChat(); strings.Contains(v, "effort:") {
		t.Error("header shows an effort level before any is set")
	}

	model, _ = model.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	am := model.(Model)
	if am.effortOverride != "none" {
		t.Fatalf("effortOverride after one ctrl+t = %q, want none", am.effortOverride)
	}
	if v := am.viewChat(); !strings.Contains(v, "effort: none") {
		t.Error("header does not show the overridden effort")
	}

	// Four more steps reach xhigh; one more wraps back to the model default.
	for range 4 {
		model, _ = model.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	}
	if got := model.(Model).effortOverride; got != "xhigh" {
		t.Fatalf("effortOverride after five ctrl+t = %q, want xhigh", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	am = model.(Model)
	if am.effortOverride != "" {
		t.Fatalf("effortOverride after full cycle = %q, want \"\"", am.effortOverride)
	}
	if am.chatNotice != "reasoning effort: model default" {
		t.Errorf("chatNotice = %q, want the model-default notice", am.chatNotice)
	}
}

func TestStreamCarriesConfiguredEffortAndOverride(t *testing.T) {
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

	// The model's configured default applies with no override.
	model = typeText(model, "hi")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	model = pumpStream(t, model)
	for _, want := range []string{`"effort":"medium"`, `"verbosity":"low"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("request missing %s:\n%s", want, body)
		}
	}

	// A session override (one ctrl+t → "none") wins over the config.
	model, _ = model.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	model = typeText(model, "again")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	pumpStream(t, model)
	if !strings.Contains(string(body), `"effort":"none"`) {
		t.Errorf("request did not carry the session override:\n%s", body)
	}
}
