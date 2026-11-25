package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	value, err := translateWithModel(context.Background(), server.Client(), profile, "第一行\n第二行", "English")
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
	if _, err := translateWithModel(context.Background(), newModelHTTPClient(), profile, "private text", "English"); err == nil {
		t.Fatal("redirect was accepted")
	}
}

func TestModelRequestsRejectNonOverlayCaller(t *testing.T) {
	service := &ModelService{app: &App{capturing: true, overlayCommitted: true, overlayPreviewID: "ready"}}
	if _, err := service.TranslateText(context.Background(), "revision", "private text", "English"); err == nil {
		t.Fatal("non-overlay translation call was allowed")
	}
	if _, err := service.RecognizeImage(context.Background(), "revision", "private screenshot"); err == nil {
		t.Fatal("non-overlay model OCR call was allowed")
	}
}
