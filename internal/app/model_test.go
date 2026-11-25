package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

const fixtureModelKey = "stub-authorization-value"

type modelTestKeychain map[string][]byte

func (keychain modelTestKeychain) Get(key string) ([]byte, error) {
	if value, ok := keychain[key]; ok {
		return value, nil
	}
	return nil, mygo.ErrSecretNotFound
}

func (keychain modelTestKeychain) Set(key string, value []byte) error {
	keychain[key] = value
	return nil
}

func (keychain modelTestKeychain) Delete(key string) error {
	delete(keychain, key)
	return nil
}

func TestModelSettingsKeepKeyOnlyForTheSameEndpoint(t *testing.T) {
	keychain := modelTestKeychain{}
	service := &ModelService{secrets: keychain}
	initial, err := service.save(ModelInput{
		Mode: "cloud", BaseURL: "https://model.example.test/v1/", Model: "first-model", APIKey: fixtureModelKey,
	})
	if err != nil || initial.BaseURL != "https://model.example.test/v1" || !initial.HasAPIKey || initial.Revision == "" {
		t.Fatalf("first save returned %+v, %v", initial, err)
	}
	encoded, err := json.Marshal(initial)
	if err != nil || strings.Contains(string(encoded), fixtureModelKey) {
		t.Fatalf("key leaked in the settings response, marshal error: %v", err)
	}
	updated, err := service.save(ModelInput{Mode: "cloud", BaseURL: initial.BaseURL, Model: "second-model"})
	profile, readErr := service.load()
	if err != nil || readErr != nil || !updated.HasAPIKey || updated.Revision == initial.Revision || profile.APIKey != fixtureModelKey {
		t.Fatal("editing the model at the same endpoint did not preserve the credential")
	}
	if _, err := service.save(ModelInput{Mode: "cloud", BaseURL: "https://other.example.test/v1", Model: "second-model"}); err == nil {
		t.Fatal("a different provider could reuse the old key")
	}
	if _, err := service.profileForRevision(initial.Revision); err == nil {
		t.Fatal("a request for an earlier settings revision was accepted")
	}
	if _, err := service.save(ModelInput{Mode: "off"}); err != nil || len(keychain) != 0 {
		t.Fatalf("disabling did not remove the Keychain item: %v", err)
	}
}

func TestModelSettingsValidateDestination(t *testing.T) {
	tests := []struct{ mode, input, want string }{
		{"local", "http://localhost:11434", "http://127.0.0.1:11434/v1"},
		{"local", "http://[::1]:1234/v1/", "http://[::1]:1234/v1"},
		{"cloud", "https://example.test/compatible/v1", "https://example.test/compatible/v1"},
		{"cloud", "https://api.openai.com", "https://api.openai.com/v1"},
		{"cloud", "https://api.openai.com/", "https://api.openai.com/v1"},
		{"local", "http://localhost.example.test:11434/v1", ""},
		{"local", "http://192.168.1.10:11434/v1", ""},
		{"cloud", "http://example.test/v1", ""},
		{"cloud", "https://name@example.test/v1", ""},
		{"cloud", "https://example.test/v1?key=secret", ""},
	}
	for _, item := range tests {
		t.Run(item.input, func(t *testing.T) {
			got, err := normalizeModelInput(ModelInput{Mode: item.mode, BaseURL: item.input, Model: "sample"})
			if item.want == "" {
				if err == nil {
					t.Fatal("invalid destination was accepted")
				}
				return
			}
			if err != nil || got.BaseURL != item.want {
				t.Fatalf("base URL = %q, error = %v", got.BaseURL, err)
			}
		})
	}
}

func TestModelTranslationSendsOnlyText(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/compatible/v1/chat/completions" || request.Header.Get("Authorization") != "Bearer "+fixtureModelKey {
			t.Error("translation used the wrong route or authentication")
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("expected text-only messages: %v", err)
		} else if body.Model != "local-test-model" || len(body.Messages) != 2 || body.Messages[1].Role != "user" || body.Messages[1].Content != "第一行\n第二行" || !strings.Contains(body.Messages[0].Content, "English") {
			t.Errorf("unexpected translation request: %+v", body)
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"First line\nSecond line"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	profile := modelProfile{ModelInput: ModelInput{Mode: "cloud", BaseURL: server.URL + "/compatible/v1", Model: "local-test-model", APIKey: fixtureModelKey}}
	value, err := translateWithModel(context.Background(), server.Client(), profile, "第一行\n第二行", "en", "auto", "", nil)
	if err != nil || value != "First line\nSecond line" {
		t.Fatalf("translation = %q, %v", value, err)
	}
}

func TestModelOCRSendsTheSelectionAsPNG(t *testing.T) {
	var encoded strings.Builder
	encoder := base64.NewEncoder(base64.StdEncoding, &encoded)
	if err := png.Encode(encoder, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct{ Content json.RawMessage }
		}
		if request.Header.Get("Authorization") != "" {
			t.Error("local model got an unexpected Authorization header")
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil || len(body.Messages) != 2 {
			t.Errorf("invalid vision messages: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		var parts []struct {
			Type     string
			ImageURL struct{ URL string } `json:"image_url"`
		}
		if err := json.Unmarshal(body.Messages[1].Content, &parts); err != nil || len(parts) != 1 || parts[0].Type != "image_url" || parts[0].ImageURL.URL != "data:image/png;base64,"+encoded.String() {
			t.Errorf("invalid screenshot input: %v", err)
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"手写文字"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	profile := modelProfile{ModelInput: ModelInput{Mode: "local", BaseURL: server.URL + "/v1", Model: "vision-model"}}
	value, err := recognizeWithModel(context.Background(), newModelHTTPClient(), profile, encoded.String())
	if err != nil || value != "手写文字" {
		t.Fatalf("model OCR = %q, %v", value, err)
	}
}

func TestModelRequestsDoNotFollowRedirects(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Error("model content was redirected to a different server")
	}))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	profile := modelProfile{ModelInput: ModelInput{Mode: "local", BaseURL: server.URL + "/v1", Model: "sample"}}
	if _, err := translateWithModel(context.Background(), newModelHTTPClient(), profile, "private text", "en", "auto", "", nil); err == nil {
		t.Fatal("redirect was accepted")
	}
}

func TestModelRequestsRejectNonOverlayCaller(t *testing.T) {
	service := &ModelService{app: &App{capturing: true, overlayCommitted: true, overlayPreviewID: "ready"}}
	if _, err := service.TranslateText(context.Background(), "revision", "private text", "en", "auto", "", nil); err == nil {
		t.Fatal("non-overlay translation call was allowed")
	}
	if _, err := service.RecognizeImage(context.Background(), "revision", "private screenshot"); err == nil {
		t.Fatal("non-overlay model OCR call was allowed")
	}
}
func TestTranslateStreamsDeltas(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body struct {
			Stream bool `json:"stream"`
		}
		if json.NewDecoder(request.Body).Decode(&body) != nil || !body.Stream {
			t.Error("translation request did not ask for a stream")
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
		fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"，世界\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()
	profile := modelProfile{ModelInput: ModelInput{Mode: "cloud", BaseURL: server.URL + "/v1", Model: "stream-model", APIKey: fixtureModelKey}}
	var deltas []string
	value, err := translateWithModelStream(context.Background(), server.Client(), profile, "hello", "zh-Hans", "auto", "", nil, func(delta string) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("streaming translation error = %v", err)
	}
	if value != "你好，世界" {
		t.Fatalf("translation = %q", value)
	}
	if strings.Join(deltas, "") != "你好，世界" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestTranslateWindowCallerAuthorization(t *testing.T) {
	mainWin := new(mygo.Window)
	translateWin := new(mygo.Window)
	otherWin := new(mygo.Window)
	app := &App{main: mainWin, translate: translateWin}
	service := &ModelService{app: app, secrets: modelTestKeychain{}}

	ctx := context.Background()
	saved, err := service.saveSettingsFor(ctx, mainWin, ModelInput{
		Mode: "cloud", BaseURL: "https://model.example.test/v1", Model: "sample", APIKey: fixtureModelKey,
	})
	if err != nil || saved.Revision == "" {
		t.Fatalf("saveSettingsFor(main) = %+v, %v", saved, err)
	}

	if got, err := service.settingsFor(ctx, mainWin); err != nil || got.Revision != saved.Revision {
		t.Fatalf("settingsFor(main) = %+v, %v", got, err)
	}
	if got, err := service.settingsFor(ctx, translateWin); err != nil || got.Revision != saved.Revision {
		t.Fatalf("settingsFor(translate) = %+v, %v", got, err)
	}
	if _, err := service.settingsFor(ctx, nil); err != errRecognitionSelection {
		t.Fatalf("settingsFor(nil) error = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.settingsFor(ctx, otherWin); err != errRecognitionSelection {
		t.Fatalf("settingsFor(other) error = %v, want %v", err, errRecognitionSelection)
	}

	if _, err := service.saveSettingsFor(ctx, translateWin, ModelInput{Mode: "off"}); err == nil {
		t.Fatal("saveSettingsFor(translate) was allowed")
	}
	if _, err := service.saveSettingsFor(ctx, otherWin, ModelInput{Mode: "off"}); err == nil {
		t.Fatal("saveSettingsFor(other) was allowed")
	}

	if _, err := service.startTranslationFor(ctx, nil, saved.Revision, "hello", "zh-Hans", "auto", "", nil); err != errRecognitionSelection {
		t.Fatalf("startTranslationFor(nil) error = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.startTranslationFor(ctx, otherWin, saved.Revision, "hello", "zh-Hans", "auto", "", nil); err != errRecognitionSelection {
		t.Fatalf("startTranslationFor(other) error = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.startTranslationFor(ctx, translateWin, saved.Revision, "   ", "zh-Hans", "auto", "", nil); err == nil || err.Error() != "没有可翻译的文字" {
		t.Fatalf("startTranslationFor(translate) error = %v, want 没有可翻译的文字", err)
	}
	if _, err := service.startTranslationFor(ctx, translateWin, saved.Revision, "hello", "zh-Hans", "auto", "casual", nil); err == nil || err.Error() != "请选择有效的翻译风格" {
		t.Fatalf("startTranslationFor(invalid style) error = %v, want 请选择有效的翻译风格", err)
	}

	app.mu.Lock()
	app.quitting = true
	app.mu.Unlock()
	if _, err := service.settingsFor(ctx, translateWin); err != errRecognitionSelection {
		t.Fatalf("settingsFor(translate while quitting) error = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.startTranslationFor(ctx, translateWin, saved.Revision, "hello", "zh-Hans", "auto", "", nil); err != errRecognitionSelection {
		t.Fatalf("startTranslationFor(translate while quitting) error = %v, want %v", err, errRecognitionSelection)
	}
}

func TestTranslationPromptStyles(t *testing.T) {
	base, err := translationPrompt("zh-Hans", "auto", "")
	if err != nil {
		t.Fatalf("default style error = %v", err)
	}
	standard, err := translationPrompt("zh-Hans", "auto", "standard")
	if err != nil || standard != base {
		t.Fatalf("standard = %q (%v), want %q", standard, err, base)
	}
	polish, err := translationPrompt("en", "zh-Hans", "polish")
	if err != nil || !strings.Contains(polish, "naturally and idiomatically") || !strings.Contains(polish, "Keep code, URLs, file paths and @mentions verbatim.") {
		t.Fatalf("polish prompt = %q, %v", polish, err)
	}
	technical, err := translationPrompt("zh-Hans", "en", "technical")
	if err != nil || !strings.Contains(technical, "technical documentation") || !strings.Contains(technical, "Keep code, URLs, file paths and @mentions verbatim.") {
		t.Fatalf("technical prompt = %q, %v", technical, err)
	}
	summary, err := translationPrompt("zh-Hans", "auto", "summary")
	if err != nil || !strings.Contains(summary, "Summarize the key points") || !strings.Contains(summary, "Keep code, URLs, file paths and @mentions verbatim.") {
		t.Fatalf("summary prompt = %q, %v", summary, err)
	}
	if _, err := translationPrompt("zh-Hans", "auto", "casual"); err == nil || err.Error() != "请选择有效的翻译风格" {
		t.Fatalf("invalid style error = %v", err)
	}
}

func TestModelPresetsLifecycleAndLegacyMigration(t *testing.T) {
	mainWin := new(mygo.Window)
	keychain := modelTestKeychain{}
	app := &App{main: mainWin}
	service := &ModelService{app: app, secrets: keychain}
	ctx := context.Background()

	// Seed a legacy single-profile entry.
	legacyProfile := modelProfile{
		ModelInput: ModelInput{Mode: "cloud", BaseURL: "https://legacy.example.test/v1", Model: "legacy-model", APIKey: fixtureModelKey},
		Revision:   "legacy-rev",
	}
	legacyBytes, _ := json.Marshal(legacyProfile)
	keychain[modelSettingsKey] = legacyBytes

	loaded, err := service.settingsFor(ctx, mainWin)
	if err != nil || loaded.ActiveID != "default" || len(loaded.Presets) != 1 || loaded.Presets[0].Name != "legacy-model" {
		t.Fatalf("legacy settingsFor = %+v, %v", loaded, err)
	}

	// Add a second local preset; legacy entry migrates out of modelSettingsKey.
	added, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
		Name: "本地 Ollama", Mode: "local", BaseURL: "http://127.0.0.1:11434", Model: "qwen2.5",
	})
	if err != nil || len(added.Presets) != 2 || added.ActiveID == "default" || added.Mode != "local" {
		t.Fatalf("savePresetFor(new) = %+v, %v", added, err)
	}
	if _, legacyStillExists := keychain[modelSettingsKey]; legacyStillExists {
		t.Fatal("legacy modelSettingsKey was not removed after migration")
	}

	// Switch back to the legacy preset and update its model while keeping its key.
	switched, err := service.selectPresetFor(ctx, mainWin, "default")
	if err != nil || switched.ActiveID != "default" || switched.Model != "legacy-model" {
		t.Fatalf("selectPresetFor(default) = %+v, %v", switched, err)
	}
	updated, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
		ID: "default", Name: "Legacy Updated", Mode: "cloud", BaseURL: "https://legacy.example.test/v1", Model: "legacy-v2",
	})
	if err != nil || !updated.HasAPIKey || updated.Presets[0].Name != "Legacy Updated" || updated.Presets[0].Model != "legacy-v2" {
		t.Fatalf("savePresetFor(update) = %+v, %v", updated, err)
	}

	// Delete active preset falls back to the remaining preset.
	afterDelete, err := service.deletePresetFor(ctx, mainWin, "default")
	if err != nil || len(afterDelete.Presets) != 1 || afterDelete.ActiveID == "" || afterDelete.Model != "qwen2.5" {
		t.Fatalf("deletePresetFor(default) = %+v, %v", afterDelete, err)
	}

	// Enforce maxModelPresets limit.
	for i := 1; i < maxModelPresets; i++ {
		if _, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
			Name: fmt.Sprintf("Extra %d", i), Mode: "local", BaseURL: "http://127.0.0.1:11434", Model: fmt.Sprintf("m-%d", i),
		}); err != nil {
			t.Fatalf("savePresetFor #%d failed: %v", i, err)
		}
	}
	if _, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
		Name: "Too Many", Mode: "local", BaseURL: "http://127.0.0.1:11434", Model: "overflow",
	}); err == nil {
		t.Fatal("expected maxModelPresets limit error")
	}
}

func TestModelPresetCallerAuthorization(t *testing.T) {
	mainWin := new(mygo.Window)
	translateWin := new(mygo.Window)
	otherWin := new(mygo.Window)
	app := &App{main: mainWin, translate: translateWin}
	service := &ModelService{app: app, secrets: modelTestKeychain{}}
	ctx := context.Background()

	saved, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
		Name: "Local", Mode: "local", BaseURL: "http://127.0.0.1:11434", Model: "m1",
	})
	if err != nil {
		t.Fatalf("savePresetFor(main) = %v", err)
	}

	if _, err := service.savePresetFor(ctx, translateWin, ModelPresetInput{Name: "Bad", Mode: "local", BaseURL: "http://127.0.0.1:11434", Model: "m2"}); err == nil {
		t.Fatal("savePresetFor(translate) was allowed")
	}
	if _, err := service.deletePresetFor(ctx, translateWin, saved.ActiveID); err == nil {
		t.Fatal("deletePresetFor(translate) was allowed")
	}
	if _, err := service.testConnectionFor(ctx, translateWin, ModelPresetInput{Mode: "local", BaseURL: "http://127.0.0.1:11434", Model: "m1"}); err == nil {
		t.Fatal("testConnectionFor(translate) was allowed")
	}
	if _, err := service.selectPresetFor(ctx, otherWin, saved.ActiveID); err != errRecognitionSelection {
		t.Fatalf("selectPresetFor(other) = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.selectPresetFor(ctx, translateWin, ""); err == nil {
		t.Fatal("selectPresetFor(translate, empty) was allowed")
	}
	if got, err := service.selectPresetFor(ctx, translateWin, saved.ActiveID); err != nil || got.ActiveID != saved.ActiveID {
		t.Fatalf("selectPresetFor(translate, valid) = %+v, %v", got, err)
	}
}

func TestModelConnectionTest(t *testing.T) {
	mainWin := new(mygo.Window)
	app := &App{main: mainWin}
	keychain := modelTestKeychain{}
	service := &ModelService{app: app, secrets: keychain}
	ctx := context.Background()

	var authStatus int = http.StatusOK
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+fixtureModelKey {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if authStatus != http.StatusOK {
			writer.WriteHeader(authStatus)
			return
		}
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	saved, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
		Name: "Cloud", Mode: "cloud", BaseURL: server.URL + "/v1", Model: "test-model", APIKey: fixtureModelKey,
	})
	if err != nil {
		t.Fatalf("savePresetFor = %v", err)
	}

	// Reuses saved key when testing the same preset & endpoint without re-entering APIKey.
	msg, err := service.testConnectionWithClient(ctx, mainWin, server.Client(), ModelPresetInput{
		ID: saved.ActiveID, Mode: "cloud", BaseURL: server.URL + "/v1", Model: "test-model",
	})
	if err != nil || !strings.HasPrefix(msg, "连接成功（") {
		t.Fatalf("testConnectionWithClient = %q, %v", msg, err)
	}

	// Different endpoint must not reuse saved key.
	if _, err := service.testConnectionWithClient(ctx, mainWin, server.Client(), ModelPresetInput{
		ID: saved.ActiveID, Mode: "cloud", BaseURL: "https://other.example.test/v1", Model: "test-model",
	}); err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("expected API Key error for changed endpoint, got %v", err)
	}

	// 401 maps to friendly auth error.
	authStatus = http.StatusUnauthorized
	if _, err := service.testConnectionWithClient(ctx, mainWin, server.Client(), ModelPresetInput{
		ID: saved.ActiveID, Mode: "cloud", BaseURL: server.URL + "/v1", Model: "test-model",
	}); err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("expected auth error on 401, got %v", err)
	}
}

func TestDictionaryQueryAndGlossaryPrompts(t *testing.T) {
	for _, query := range []string{"ephemeral", "rate limit", "self-hosted", "don't", "幂等性"} {
		if !looksLikeDictionaryQuery(query) {
			t.Errorf("looksLikeDictionaryQuery(%q) = false, want true", query)
		}
	}
	for _, nonQuery := range []string{
		"", "Hello world.", "one\ntwo", "https://example.test", "/usr/local/bin",
		"@alice", "one two three four", "foo_bar", "这是一个比较长的中文句子超过六个字",
	} {
		if looksLikeDictionaryQuery(nonQuery) {
			t.Errorf("looksLikeDictionaryQuery(%q) = true, want false", nonQuery)
		}
	}

	dictPrompt, err := translationPromptForText("ephemeral", "zh-Hans", "en", "standard", nil)
	if err != nil || !strings.Contains(dictPrompt, "dictionary entry") || !strings.Contains(dictPrompt, "pronunciation/phonetics") {
		t.Fatalf("dictionary prompt = %q, %v", dictPrompt, err)
	}

	techPrompt, err := translationPromptForText("ephemeral", "zh-Hans", "en", "technical", nil)
	if err != nil || strings.Contains(techPrompt, "dictionary entry") || !strings.Contains(techPrompt, "technical documentation") {
		t.Fatalf("technical style should not switch to dictionary prompt: %q, %v", techPrompt, err)
	}

	glossary := []GlossaryEntry{
		{Source: "Pinstill", Target: ""},
		{Source: "rate limit", Target: "速率限制"},
		{Source: "UnusedTerm", Target: "不会出现"},
	}
	withGlossary, err := translationPromptForText("Pinstill enforces a RATE LIMIT here.", "zh-Hans", "en", "standard", glossary)
	if err != nil {
		t.Fatalf("translationPromptForText with glossary error = %v", err)
	}
	if !strings.Contains(withGlossary, "- Pinstill => keep verbatim") || !strings.Contains(withGlossary, "- rate limit => 速率限制") {
		t.Fatalf("matched glossary entries missing in prompt: %q", withGlossary)
	}
	if strings.Contains(withGlossary, "UnusedTerm") {
		t.Fatalf("unmatched glossary entry should not be injected: %q", withGlossary)
	}
}

type indexFailingModelKeychain struct {
	modelTestKeychain
	fail bool
}

func (keychain *indexFailingModelKeychain) Set(key string, value []byte) error {
	if keychain.fail && key == modelIndexKey {
		return fmt.Errorf("fixture: index write failed")
	}
	return keychain.modelTestKeychain.Set(key, value)
}

func TestModelPresetMigrationKeepsLegacyOnFailedIndexWrite(t *testing.T) {
	for _, operation := range []string{"save", "select"} {
		t.Run(operation, func(t *testing.T) {
			legacy := modelProfile{
				ModelInput: ModelInput{Mode: "cloud", BaseURL: "https://legacy.example.test/v1", Model: "legacy-model", APIKey: fixtureModelKey},
				Revision:   "legacy-rev",
			}
			data, err := json.Marshal(legacy)
			if err != nil {
				t.Fatal(err)
			}
			keychain := &indexFailingModelKeychain{modelTestKeychain: modelTestKeychain{modelSettingsKey: data}, fail: true}
			main := new(mygo.Window)
			service := &ModelService{app: &App{main: main}, secrets: keychain}
			migrate := func() error {
				if operation == "select" {
					_, err := service.selectPresetFor(context.Background(), main, "default")
					return err
				}
				_, err := service.savePresetLocked(ModelPresetInput{ID: "default", Mode: "cloud", BaseURL: legacy.BaseURL, Model: "new-model"})
				return err
			}
			if err := migrate(); err == nil {
				t.Fatal("migration succeeded despite the failed index write")
			}
			profile, err := service.load()
			if err != nil || profile.APIKey != legacy.APIKey || profile.Revision != legacy.Revision {
				t.Fatalf("original model configuration could not be retrieved: mode=%q revision=%q err=%v", profile.Mode, profile.Revision, err)
			}
			keychain.fail = false
			if err := migrate(); err != nil {
				t.Fatalf("migration retry: %v", err)
			}
			if _, exists := keychain.modelTestKeychain[modelSettingsKey]; exists {
				t.Fatal("legacy record remained after successful migration")
			}
		})
	}
}
