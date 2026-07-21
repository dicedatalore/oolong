package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/zalando/go-keyring"

	"github.com/dicedatalore/oolong/internal/config"
	"github.com/dicedatalore/oolong/internal/keystore"
	"github.com/dicedatalore/oolong/internal/provider/anthropic"
	"github.com/dicedatalore/oolong/internal/provider/google"
)

func newKeyManagerModel(t *testing.T) tea.Model {
	t.Helper()
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	if model.(Model).state != statePicker {
		t.Fatal("a keyless launch did not remain on the picker")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if model.(Model).state != stateKeyManager {
		t.Fatal("ctrl+k did not open the key manager")
	}
	return model
}

func TestNoKeysPromptForKeyManager(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	var model tea.Model = New(nil, "dark", config.Config{}, "")
	model, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 28})
	am := model.(Model)
	if am.state != statePicker {
		t.Fatalf("state = %v, want picker", am.state)
	}
	if got := len(am.picker.Items()); got != 0 {
		t.Errorf("keyless picker shows %d default models, want none", got)
	}
	if !strings.Contains(am.viewPicker(), "ctrl+k opens the key manager") {
		t.Error("keyless picker does not prompt for the key manager")
	}
}

func TestDefaultModelsAppearOnlyForRelevantKey(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	model := New(nil, "dark", config.Config{}, "")
	if got := len(model.picker.Items()); got != 0 {
		t.Fatalf("initial defaults = %d, want none", got)
	}
	if err := keystore.Set(keystore.Anthropic, "sk-ant-test"); err != nil {
		t.Fatal(err)
	}
	model.refreshBuiltinCatalog()
	if got, want := len(pickerModels(model)), builtinProviderCount("anthropic"); got != want {
		t.Errorf("Anthropic key exposed %d defaults, want %d Anthropic defaults", got, want)
	}
	if err := keystore.Set(keystore.OpenAI, "sk-test"); err != nil {
		t.Fatal(err)
	}
	model.refreshBuiltinCatalog()
	if got, want := len(pickerModels(model)), builtinProviderCount("anthropic")+builtinProviderCount("openai"); got != want {
		t.Errorf("OpenAI key exposed %d defaults, want %d", got, want)
	}
	if err := keystore.Set(keystore.Google, "AIza-test"); err != nil {
		t.Fatal(err)
	}
	model.refreshBuiltinCatalog()
	if got := len(pickerModels(model)); got != len(config.DefaultModels) {
		t.Errorf("Google key exposed %d defaults, want all %d", got, len(config.DefaultModels))
	}
}

func TestAnthropicModelUsesAnthropicClient(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	cfg := config.Config{Models: []config.Model{{ID: "claude-test", Provider: "anthropic"}}}
	model := New(nil, "dark", cfg, "")
	if _, ok := model.clientFor("claude-test").(*anthropic.Client); !ok {
		t.Error("Anthropic model was not routed to the Anthropic client")
	}
}

func TestGoogleModelUsesGoogleClient(t *testing.T) {
	keyring.MockInit()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "AIza-test")
	cfg := config.Config{Models: []config.Model{{ID: "gemini-test", Provider: "google"}}}
	model := New(nil, "dark", cfg, "")
	if _, ok := model.clientFor("gemini-test").(*google.Client); !ok {
		t.Error("Google model was not routed to the Google client")
	}
}

func TestKeyManagerCyclesThreeProviders(t *testing.T) {
	model := newKeyManagerModel(t)
	if got := model.(Model).keyProvider; got != keystore.OpenAI {
		t.Fatalf("initial provider = %q", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := model.(Model).keyProvider; got != keystore.Google {
		t.Fatalf("two tabs landed on %q, want google", got)
	}
	if v := model.(Model).viewKeyManager(); !strings.Contains(v, "Google") {
		t.Error("key manager view does not show the Google row")
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if got := model.(Model).keyProvider; got != keystore.OpenAI {
		t.Errorf("third tab landed on %q, want wrap to openai", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := model.(Model).keyProvider; got != keystore.Google {
		t.Errorf("up from openai landed on %q, want wrap to google", got)
	}
}

func TestKeyManagerUsesHumanReadableCredentialStatuses(t *testing.T) {
	model := newKeyManagerModel(t)
	am := model.(Model)
	am.keyStatuses[keystore.OpenAI] = "keychain"
	view := am.viewKeyManager()
	if !strings.Contains(view, "Saved on this device") || strings.Contains(view, "(keychain)") {
		t.Errorf("saved status is unclear: %q", view)
	}

	t.Setenv("OPENAI_API_KEY", "sk-env-test")
	am.refreshKeyStatuses()
	view = am.viewKeyManager()
	if !strings.Contains(view, "Provided by OPENAI_API_KEY") {
		t.Errorf("environment status does not name its source: %q", view)
	}
}

func TestKeyManagerArrowKeysSelectProvider(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if got := model.(Model).keyProvider; got != keystore.Anthropic {
		t.Fatalf("right selected %q, want anthropic", got)
	}
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if got := model.(Model).keyProvider; got != keystore.OpenAI {
		t.Fatalf("left selected %q, want openai", got)
	}
}

func TestKeyManagerCardFitsNarrowTerminal(t *testing.T) {
	model := newKeyManagerModel(t)
	am := model.(Model)
	am.width = 40
	if got := lipgloss.Width(am.keyCard()); got > am.width {
		t.Errorf("card width = %d, terminal width = %d", got, am.width)
	}
}

func TestKeyManagerSurvivesCompactTerminal(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.WindowSizeMsg{Width: 24, Height: 10})
	am := model.(Model)
	if width := lipgloss.Width(am.keyCard()); width > 24 {
		t.Errorf("compact key card width = %d", width)
	}
	view := am.viewKeyManager()
	if width, height := lipgloss.Width(view), lipgloss.Height(view); width > 24 || height > 10 {
		t.Errorf("compact key manager rendered %dx%d", width, height)
	}
}

func TestKeyManagerCardBordersAlign(t *testing.T) {
	model := newKeyManagerModel(t)
	lines := strings.Split(model.(Model).keyCard(), "\n")
	if len(lines) < 2 {
		t.Fatalf("card rendered only %d line(s)", len(lines))
	}
	if top, side := strings.Index(lines[0], "╭"), strings.Index(lines[1], "│"); top != side {
		t.Errorf("top border starts at column %d, side border at %d", top, side)
	}
}

func TestKeyManagerContentIsCenteredWithHelpAtBottom(t *testing.T) {
	model := newKeyManagerModel(t)
	am := model.(Model)
	plain := ansi.ReplaceAllString(am.viewKeyManager(), "")
	lines := strings.Split(plain, "\n")

	cardLine, helpLine := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "╭") {
			cardLine = i
		}
		if strings.Contains(line, "verify & save") {
			helpLine = i
		}
	}
	if cardLine < 0 || helpLine < 0 {
		t.Fatalf("missing card or help line:\n%s", plain)
	}
	line := lines[cardLine]
	lead := len(line) - len(strings.TrimLeft(line, " "))
	trail := len(line) - len(strings.TrimRight(line, " "))
	if diff := lead - trail; diff < -1 || diff > 1 {
		t.Errorf("card is not centered: %d leading vs %d trailing spaces", lead, trail)
	}
	if helpLine <= cardLine || len(lines)-helpLine > 3 {
		t.Errorf("help line %d is not pinned below the card near the bottom of %d lines", helpLine, len(lines))
	}
	help := lines[helpLine]
	helpLead := len(help) - len(strings.TrimLeft(help, " "))
	helpTrail := len(help) - len(strings.TrimRight(help, " "))
	if diff := helpLead - helpTrail; diff < -1 || diff > 1 {
		t.Errorf("help is not centered: %d leading vs %d trailing spaces", helpLead, helpTrail)
	}
}

func builtinProviderCount(provider string) int {
	count := 0
	for _, model := range config.DefaultModels {
		if model.Provider == provider {
			count++
		}
	}
	return count
}

func TestKeyManagerRejectsEmptyKey(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	am := model.(Model)
	if am.keyErr == "" || am.keyValidating {
		t.Errorf("empty key state: error=%q validating=%v", am.keyErr, am.keyValidating)
	}
}

func TestClosingKeyManagerRestartsPickerSparkles(t *testing.T) {
	model := newKeyManagerModel(t)
	before := model.(Model).sparkleTag
	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	am := model.(Model)
	if am.state != statePicker {
		t.Fatalf("state = %v, want picker", am.state)
	}
	if am.sparkleTag != before+1 {
		t.Errorf("sparkle tag = %d, want %d", am.sparkleTag, before+1)
	}
	if cmd == nil {
		t.Error("closing key manager did not schedule a new sparkle tick")
	}
}

func TestOpenAIValidationFailureStaysInManager(t *testing.T) {
	model := newKeyManagerModel(t)
	model = typeText(model, "sk-bad")
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).keyValidating {
		t.Fatal("OpenAI key did not start validation")
	}
	model, _ = model.Update(keyCheckMsg{provider: keystore.OpenAI, key: "sk-bad", err: errors.New("invalid API key")})
	am := model.(Model)
	if am.state != stateKeyManager || !strings.Contains(am.keyErr, "invalid API key") || !strings.Contains(am.keyErr, "try again") {
		t.Errorf("state=%v error=%q", am.state, am.keyErr)
	}
}

func TestAnthropicKeySavedOnlyToKeychainAndInputCleared(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = typeText(model, "sk-ant-test")
	model, _ = model.Update(keyCheckMsg{provider: keystore.Anthropic, key: "sk-ant-test"})
	am := model.(Model)
	if am.anthropicKeyInput.Value() != "" {
		t.Error("Anthropic input retained the saved secret")
	}
	got, err := keystore.Get(keystore.Anthropic)
	if err != nil || got != "sk-ant-test" {
		t.Fatalf("keychain value = %q, %v", got, err)
	}
	if am.state != statePicker {
		t.Error("saving a key did not return to the model picker")
	}
	if !strings.Contains(am.keyNotice, "choose a model") {
		t.Errorf("success notice = %q, want a next step", am.keyNotice)
	}
}

func TestAnthropicKeyStartsValidation(t *testing.T) {
	model := newKeyManagerModel(t)
	model, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = typeText(model, "sk-ant-test")
	model, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !model.(Model).keyValidating || cmd == nil {
		t.Error("Anthropic key did not start asynchronous validation")
	}
}
