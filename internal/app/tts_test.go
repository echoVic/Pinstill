package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo"
)

func TestTTSSettingsValidationAndKeyReuse(t *testing.T) {
	mainWin := new(mygo.Window)
	keychain := modelTestKeychain{}
	app := &App{main: mainWin}
	service := &ModelService{app: app, secrets: keychain}
	ctx := context.Background()

	// Seed a DeepSeek cloud model preset.
	if _, err := service.savePresetFor(ctx, mainWin, ModelPresetInput{
		Name: "DeepSeek", Mode: "cloud", BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", APIKey: fixtureModelKey,
	}); err != nil {
		t.Fatalf("savePresetFor = %v", err)
	}

	// Saving DeepSeek TTS without APIKey reuses the saved DeepSeek key for the same endpoint.
	saved, err := service.saveTTSSettingsFor(ctx, mainWin, TTSInput{
		Provider: "deepseek",
	})
	if err != nil || saved.Provider != "deepseek" || saved.BaseURL != "https://api.deepseek.com/v1" || saved.Voice != "stella" || !saved.HasAPIKey || saved.Revision == "" {
		t.Fatalf("saveTTSSettingsFor(deepseek) = %+v, %v", saved, err)
	}

	// OpenAI local requires loopback address and model name.
	if _, err := normalizeTTSInput(TTSInput{Provider: "openai-local", BaseURL: "http://192.168.1.5:8880", Model: "kokoro"}); err == nil {
		t.Fatal("non-loopback local TTS URL was accepted")
	}
	if _, err := normalizeTTSInput(TTSInput{Provider: "openai-local", BaseURL: "http://localhost:8880/v1/audio/speech", Model: ""}); err == nil {
		t.Fatal("empty model name was accepted for OpenAI local TTS")
	}
	localNorm, err := normalizeTTSInput(TTSInput{Provider: "openai-local", BaseURL: "http://localhost:8880/v1/audio/speech", Model: "kokoro", Voice: ""})
	if err != nil || localNorm.BaseURL != "http://127.0.0.1:8880/v1" || localNorm.Voice != "alloy" {
		t.Fatalf("normalizeTTSInput(openai-local) = %+v, %v", localNorm, err)
	}

	// Cloud OpenAI TTS must not reuse the DeepSeek key from a different host.
	if _, err := service.saveTTSSettingsFor(ctx, mainWin, TTSInput{
		Provider: "openai-cloud", BaseURL: "https://api.openai.com", Model: "gpt-4o-mini-tts",
	}); err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("expected API Key error for different cloud host, got %v", err)
	}

	// Switching back to system clears the TTS keychain entry.
	off, err := service.saveTTSSettingsFor(ctx, mainWin, TTSInput{Provider: "system"})
	if err != nil || off.Provider != "system" {
		t.Fatalf("saveTTSSettingsFor(system) = %+v, %v", off, err)
	}
	if _, exists := keychain[ttsSettingsKey]; exists {
		t.Fatal("system provider did not clear TTS keychain item")
	}
}

func TestTTSCallerAuthorization(t *testing.T) {
	mainWin := new(mygo.Window)
	translateWin := new(mygo.Window)
	otherWin := new(mygo.Window)
	app := &App{main: mainWin, translate: translateWin}
	service := &ModelService{app: app, secrets: modelTestKeychain{}}
	ctx := context.Background()

	if _, err := service.saveTTSSettingsFor(ctx, translateWin, TTSInput{Provider: "deepseek", APIKey: fixtureModelKey}); err == nil {
		t.Fatal("saveTTSSettingsFor(translate) was allowed")
	}
	if _, err := service.testTTSWithClient(ctx, translateWin, http.DefaultClient, TTSInput{Provider: "deepseek", APIKey: fixtureModelKey}); err == nil {
		t.Fatal("testTTSWithClient(translate) was allowed")
	}
	if _, err := service.ttsSettingsFor(ctx, otherWin); err != errRecognitionSelection {
		t.Fatalf("ttsSettingsFor(other) = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.ttsSettingsFor(ctx, translateWin); err != nil {
		t.Fatalf("ttsSettingsFor(translate) = %v", err)
	}
	if _, err := service.synthesizeSpeechWithClient(ctx, mainWin, http.DefaultClient, "rev", "hi"); err != errRecognitionSelection {
		t.Fatalf("synthesizeSpeechWithClient(main) = %v, want %v", err, errRecognitionSelection)
	}
	if _, err := service.synthesizeSpeechWithClient(ctx, otherWin, http.DefaultClient, "rev", "hi"); err != errRecognitionSelection {
		t.Fatalf("synthesizeSpeechWithClient(other) = %v, want %v", err, errRecognitionSelection)
	}
}

func TestSynthesizeDeepSeekAndOpenAI(t *testing.T) {
	mainWin := new(mygo.Window)
	translateWin := new(mygo.Window)
	app := &App{main: mainWin, translate: translateWin}
	service := &ModelService{app: app, secrets: modelTestKeychain{}}
	ctx := context.Background()

	var statusCode = http.StatusOK
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+fixtureModelKey {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		if statusCode != http.StatusOK {
			writer.WriteHeader(statusCode)
			return
		}
		switch request.URL.Path {
		case "/v1/tools/tts":
			var body struct {
				Text           string `json:"text"`
				VoiceID        string `json:"voice_id"`
				Format         string `json:"format"`
				FilterMarkdown bool   `json:"filter_markdown"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.VoiceID != "tide" || body.Format != "wav" || !body.FilterMarkdown || body.Text == "" {
				t.Errorf("unexpected deepseek TTS body: %+v, err=%v", body, err)
			}
			writer.Header().Set("Content-Type", "audio/wav")
			_, _ = writer.Write([]byte("RIFFdemo"))
		case "/v1/audio/speech":
			var body struct {
				Model          string `json:"model"`
				Input          string `json:"input"`
				Voice          string `json:"voice"`
				ResponseFormat string `json:"response_format"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Model != "gpt-4o-mini-tts" || body.Voice != "coral" || body.ResponseFormat != "mp3" || body.Input == "" {
				t.Errorf("unexpected openai TTS body: %+v, err=%v", body, err)
			}
			writer.Header().Set("Content-Type", "audio/mpeg")
			_, _ = writer.Write([]byte("ID3demo"))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// 1. DeepSeek TTS
	ds, err := service.saveTTSSettingsFor(ctx, mainWin, TTSInput{
		Provider: "deepseek", BaseURL: server.URL + "/v1", Voice: "tide", APIKey: fixtureModelKey,
	})
	if err != nil {
		t.Fatalf("saveTTSSettingsFor(deepseek) = %v", err)
	}
	audio, err := service.synthesizeSpeechWithClient(ctx, translateWin, server.Client(), ds.Revision, "你好 DeepSeek")
	if err != nil || audio.MimeType != "audio/wav" || audio.DataBase64 != base64.StdEncoding.EncodeToString([]byte("RIFFdemo")) {
		t.Fatalf("deepseek audio = %+v, %v", audio, err)
	}

	// 2. OpenAI-compatible TTS + testTTSWithClient
	oa, err := service.saveTTSSettingsFor(ctx, mainWin, TTSInput{
		Provider: "openai-cloud", BaseURL: server.URL + "/v1", Model: "gpt-4o-mini-tts", Voice: "coral", APIKey: fixtureModelKey,
	})
	if err != nil {
		t.Fatalf("saveTTSSettingsFor(openai-cloud) = %v", err)
	}
	audio2, err := service.testTTSWithClient(ctx, mainWin, server.Client(), TTSInput{
		Provider: "openai-cloud", BaseURL: server.URL + "/v1", Model: "gpt-4o-mini-tts", Voice: "coral",
	})
	if err != nil || audio2.MimeType != "audio/mpeg" || audio2.DataBase64 != base64.StdEncoding.EncodeToString([]byte("ID3demo")) {
		t.Fatalf("openai test audio = %+v, %v", audio2, err)
	}

	// 3. Stale revision & HTTP 429 error mapping
	if _, err := service.synthesizeSpeechWithClient(ctx, translateWin, server.Client(), ds.Revision, "stale"); err == nil {
		t.Fatal("stale TTS revision was accepted")
	}
	statusCode = http.StatusTooManyRequests
	if _, err := service.synthesizeSpeechWithClient(ctx, translateWin, server.Client(), oa.Revision, "rate limited"); err == nil || !strings.Contains(err.Error(), "请求过多") {
		t.Fatalf("expected 429 error, got %v", err)
	}
}

func TestDeepSeekSpeechChecksBilledCharactersBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFFdemo"))
	}))
	defer server.Close()
	profile := ttsProfile{TTSInput: TTSInput{Provider: "deepseek", BaseURL: server.URL, Voice: "stella"}}
	if _, err := synthesizeWithTTS(context.Background(), server.Client(), profile, strings.Repeat("山", 5000)); err != nil {
		t.Fatalf("text at limit: %v", err)
	}
	if _, err := synthesizeWithTTS(context.Background(), server.Client(), profile, strings.Repeat("山", 5000)+"a"); err == nil || !strings.Contains(err.Error(), "过长") {
		t.Fatalf("over-limit text returned %v", err)
	}
	if actual := calls.Load(); actual != 1 {
		t.Fatalf("requests sent = %d, want 1", actual)
	}
}
