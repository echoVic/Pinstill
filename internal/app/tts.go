package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
)

const (
	ttsSettingsKey          = "recognition-tts"
	defaultDeepSeek         = "https://api.deepseek.com/v1"
	maxTTSInputRunes        = 10000
	maxTTSInputBytes        = 60 << 10
	maxDeepSeekTTSBodyBytes = 64 << 10
	maxTTSAudioBytes        = 16 << 20
)

type TTSInput struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
	Voice    string `json:"voice"`
	APIKey   string `json:"apiKey"`
}

type TTSSettings struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	Voice     string `json:"voice"`
	HasAPIKey bool   `json:"hasApiKey"`
	Revision  string `json:"revision"`
}

type TTSAudio struct {
	MimeType   string `json:"mimeType"`
	DataBase64 string `json:"dataBase64"`
}

type ttsProfile struct {
	TTSInput
	Revision string `json:"revision"`
}

func (profile ttsProfile) settings() TTSSettings {
	return TTSSettings{
		Provider:  profile.Provider,
		BaseURL:   profile.BaseURL,
		Model:     profile.Model,
		Voice:     profile.Voice,
		HasAPIKey: profile.APIKey != "",
		Revision:  profile.Revision,
	}
}

func cleanVoiceName(value, fallback string) string {
	cleaned := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
	if cleaned == "" {
		return fallback
	}
	runes := []rune(cleaned)
	if len(runes) > 64 {
		return string(runes[:64])
	}
	return cleaned
}

func normalizeTTSEndpoint(raw string, localOnly bool, trimSuffix string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 512 || strings.ContainsAny(raw, "\\?#") || strings.IndexFunc(raw, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
	}) >= 0 {
		return "", errors.New("请输入不带查询参数的服务地址")
	}
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawPath != "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return "", errors.New("请输入有效的 HTTP 或 HTTPS 服务地址")
	}
	if localOnly {
		host := endpoint.Hostname()
		if strings.EqualFold(host, "localhost") {
			port := endpoint.Port()
			endpoint.Host = "127.0.0.1"
			if port != "" {
				endpoint.Host = net.JoinHostPort("127.0.0.1", port)
			}
		} else if !net.ParseIP(host).IsLoopback() {
			return "", errors.New("本地语音服务需要本机回环地址，例如 http://127.0.0.1:8880")
		}
	} else if endpoint.Scheme != "https" {
		return "", errors.New("云端语音服务需要 HTTPS 服务地址")
	}
	endpoint.Path = strings.TrimSuffix(strings.TrimRight(endpoint.Path, "/"), trimSuffix)
	if endpoint.Path == "" {
		endpoint.Path = "/v1"
	}
	return endpoint.String(), nil
}

func normalizeTTSInput(input TTSInput) (TTSInput, error) {
	provider := strings.TrimSpace(input.Provider)
	if provider == "" || provider == "system" {
		return TTSInput{Provider: "system"}, nil
	}
	input.APIKey = strings.TrimSpace(input.APIKey)
	if len(input.APIKey) > 1024 || strings.IndexFunc(input.APIKey, func(character rune) bool {
		return character < 33 || character > 126
	}) >= 0 {
		return TTSInput{}, errors.New("API Key 格式无效")
	}
	switch provider {
	case "deepseek":
		rawURL := strings.TrimSpace(input.BaseURL)
		if rawURL == "" {
			rawURL = defaultDeepSeek
		}
		baseURL, err := normalizeTTSEndpoint(rawURL, false, "/tools/tts")
		if err != nil {
			return TTSInput{}, err
		}
		return TTSInput{
			Provider: "deepseek",
			BaseURL:  baseURL,
			Voice:    cleanVoiceName(input.Voice, "stella"),
			APIKey:   input.APIKey,
		}, nil
	case "openai-local", "openai-cloud":
		baseURL, err := normalizeTTSEndpoint(input.BaseURL, provider == "openai-local", "/audio/speech")
		if err != nil {
			return TTSInput{}, err
		}
		model := strings.TrimSpace(input.Model)
		if model == "" || len(model) > 200 || strings.IndexFunc(model, unicode.IsControl) >= 0 {
			return TTSInput{}, errors.New("请输入有效的 TTS 模型名")
		}
		return TTSInput{
			Provider: provider,
			BaseURL:  baseURL,
			Model:    model,
			Voice:    cleanVoiceName(input.Voice, "alloy"),
			APIKey:   input.APIKey,
		}, nil
	default:
		return TTSInput{}, errors.New("请选择有效的语音朗读引擎")
	}
}

// Caller holds service.mu.
func (service *ModelService) loadTTSLocked() (ttsProfile, error) {
	data, err := service.secrets.Get(ttsSettingsKey)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return ttsProfile{TTSInput: TTSInput{Provider: "system"}}, nil
	}
	if err != nil {
		return ttsProfile{}, errors.New("无法从系统钥匙串读取语音配置")
	}
	var profile ttsProfile
	if json.Unmarshal(data, &profile) != nil || profile.Revision == "" {
		return ttsProfile{}, errors.New("语音配置已损坏，请重新保存配置")
	}
	profile.TTSInput, err = normalizeTTSInput(profile.TTSInput)
	return profile, err
}

// Caller holds service.mu.
func (service *ModelService) resolveReusableTTSKeyLocked(normalized TTSInput) string {
	if normalized.APIKey != "" {
		return normalized.APIKey
	}
	if previous, err := service.loadTTSLocked(); err == nil {
		if previous.Provider == normalized.Provider && previous.BaseURL == normalized.BaseURL && previous.APIKey != "" {
			return previous.APIKey
		}
	}
	if normalized.Provider == "deepseek" || normalized.Provider == "openai-cloud" {
		activeID, profiles, _, err := service.loadCatalog()
		if err == nil {
			for _, profile := range profiles {
				if profile.ID == activeID && profile.Mode == "cloud" && profile.BaseURL == normalized.BaseURL && profile.APIKey != "" {
					return profile.APIKey
				}
			}
			for _, profile := range profiles {
				if profile.Mode == "cloud" && profile.BaseURL == normalized.BaseURL && profile.APIKey != "" {
					return profile.APIKey
				}
			}
		}
	}
	return ""
}

func (service *ModelService) saveTTS(input TTSInput) (TTSSettings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	normalized, err := normalizeTTSInput(input)
	if err != nil {
		return TTSSettings{}, err
	}
	if normalized.Provider == "system" {
		if err := service.deleteKey(ttsSettingsKey); err != nil {
			return TTSSettings{}, errors.New("无法从系统钥匙串移除语音配置")
		}
		return TTSSettings{Provider: "system"}, nil
	}
	if normalized.APIKey == "" {
		normalized.APIKey = service.resolveReusableTTSKeyLocked(normalized)
	}
	if (normalized.Provider == "deepseek" || normalized.Provider == "openai-cloud") && normalized.APIKey == "" {
		return TTSSettings{}, errors.New("请输入该语音服务的 API Key")
	}
	revision, err := newRandomHex(16)
	if err != nil {
		return TTSSettings{}, errors.New("无法生成语音配置标识")
	}
	profile := ttsProfile{TTSInput: normalized, Revision: revision}
	data, err := json.Marshal(profile)
	if err != nil || len(data) > mygo.MaxSecretSize {
		return TTSSettings{}, errors.New("语音配置过长，请检查服务地址、模型名和 API Key")
	}
	if err := service.secrets.Set(ttsSettingsKey, data); err != nil {
		return TTSSettings{}, errors.New("无法将语音配置保存到系统钥匙串")
	}
	return profile.settings(), nil
}

func (service *ModelService) ttsSettingsFor(ctx context.Context, caller *mygo.Window) (TTSSettings, error) {
	if err := ctx.Err(); err != nil {
		return TTSSettings{}, err
	}
	if !service.app.isMainWindow(caller) && !service.app.isTranslateWindow(caller) {
		return TTSSettings{}, errRecognitionSelection
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	profile, err := service.loadTTSLocked()
	return profile.settings(), err
}

func (service *ModelService) TTSSettings(ctx context.Context) (TTSSettings, error) {
	return service.ttsSettingsFor(ctx, mygo.CallerWindow(ctx))
}

func (service *ModelService) saveTTSSettingsFor(ctx context.Context, caller *mygo.Window, input TTSInput) (TTSSettings, error) {
	if err := ctx.Err(); err != nil {
		return TTSSettings{}, err
	}
	if !service.app.isMainWindow(caller) {
		return TTSSettings{}, errors.New("请在偏好设置中保存语音配置")
	}
	return service.saveTTS(input)
}

func (service *ModelService) SaveTTSSettings(ctx context.Context, input TTSInput) (TTSSettings, error) {
	return service.saveTTSSettingsFor(ctx, mygo.CallerWindow(ctx), input)
}

func (service *ModelService) testTTSWithClient(ctx context.Context, caller *mygo.Window, client *http.Client, input TTSInput) (TTSAudio, error) {
	if err := ctx.Err(); err != nil {
		return TTSAudio{}, err
	}
	if !service.app.isMainWindow(caller) {
		return TTSAudio{}, errors.New("请在偏好设置中试听语音配置")
	}
	normalized, err := normalizeTTSInput(input)
	if err != nil {
		return TTSAudio{}, err
	}
	if normalized.Provider == "system" {
		return TTSAudio{}, errors.New("请先选择 DeepSeek TTS 或 OpenAI 兼容语音引擎")
	}
	if normalized.APIKey == "" {
		service.mu.Lock()
		normalized.APIKey = service.resolveReusableTTSKeyLocked(normalized)
		service.mu.Unlock()
	}
	if (normalized.Provider == "deepseek" || normalized.Provider == "openai-cloud") && normalized.APIKey == "" {
		return TTSAudio{}, errors.New("请输入该语音服务的 API Key")
	}
	return synthesizeWithTTS(ctx, client, ttsProfile{TTSInput: normalized}, "Hello, Pinstill TTS works.")
}

func (service *ModelService) TestTTS(ctx context.Context, input TTSInput) (TTSAudio, error) {
	return service.testTTSWithClient(ctx, mygo.CallerWindow(ctx), modelHTTPClient, input)
}

func (service *ModelService) synthesizeSpeechWithClient(ctx context.Context, caller *mygo.Window, client *http.Client, revision, text string) (TTSAudio, error) {
	if err := ctx.Err(); err != nil {
		return TTSAudio{}, err
	}
	if !service.app.isTranslateWindow(caller) {
		return TTSAudio{}, errRecognitionSelection
	}
	service.mu.Lock()
	profile, err := service.loadTTSLocked()
	service.mu.Unlock()
	if err != nil {
		return TTSAudio{}, err
	}
	if profile.Provider == "system" {
		return TTSAudio{}, errors.New("未启用远程 TTS 语音服务")
	}
	if revision == "" || profile.Revision != revision {
		return TTSAudio{}, errors.New("语音配置已更改，请重新尝试")
	}
	return synthesizeWithTTS(ctx, client, profile, text)
}

func (service *ModelService) SynthesizeSpeech(ctx context.Context, revision, text string) (TTSAudio, error) {
	return service.synthesizeSpeechWithClient(ctx, mygo.CallerWindow(ctx), modelHTTPClient, revision, text)
}

func ttsStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("语音服务拒绝访问，请检查 API Key 和权限")
	case http.StatusNotFound:
		return errors.New("语音接口或模型不存在，请检查服务地址和配置")
	case http.StatusTooManyRequests:
		return errors.New("语音服务请求过多，请稍后重试")
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return errors.New("语音服务无法处理该文本，请检查参数或文本内容")
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return errors.New("语音服务繁忙或超时，请稍后重试")
	}
	if status >= 300 && status < 400 {
		return errors.New("服务要求重定向，请填写最终服务地址")
	}
	return fmt.Errorf("语音服务请求失败（HTTP %d）", status)
}

func synthesizeWithTTS(ctx context.Context, client *http.Client, profile ttsProfile, text string) (TTSAudio, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return TTSAudio{}, errors.New("没有可朗读的文字")
	}
	characters := utf8.RuneCountInString(trimmed)
	if profile.Provider == "deepseek" {
		// DeepSeek's full-text endpoint counts Han characters twice.
		for _, char := range trimmed {
			if unicode.Is(unicode.Han, char) {
				characters++
			}
		}
	}
	if characters > maxTTSInputRunes || len(trimmed) > maxTTSInputBytes {
		return TTSAudio{}, errors.New("朗读文本过长，请分段朗读")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var endpoint string
	var payload any
	var defaultMIME string
	switch profile.Provider {
	case "deepseek":
		endpoint = profile.BaseURL + "/tools/tts"
		defaultMIME = "audio/wav"
		payload = struct {
			Text           string `json:"text"`
			VoiceID        string `json:"voice_id"`
			Format         string `json:"format"`
			FilterMarkdown bool   `json:"filter_markdown"`
		}{
			Text:           trimmed,
			VoiceID:        profile.Voice,
			Format:         "wav",
			FilterMarkdown: true,
		}
	case "openai-local", "openai-cloud":
		endpoint = profile.BaseURL + "/audio/speech"
		defaultMIME = "audio/mpeg"
		payload = struct {
			Model          string `json:"model"`
			Input          string `json:"input"`
			Voice          string `json:"voice"`
			ResponseFormat string `json:"response_format"`
		}{
			Model:          profile.Model,
			Input:          trimmed,
			Voice:          profile.Voice,
			ResponseFormat: "mp3",
		}
	default:
		return TTSAudio{}, errors.New("请选择有效的语音朗读引擎")
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return TTSAudio{}, errors.New("无法准备语音请求")
	}
	if profile.Provider == "deepseek" && len(bodyBytes) > maxDeepSeekTTSBodyBytes {
		return TTSAudio{}, errors.New("朗读文本过长，请分段朗读")
	}
	request, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return TTSAudio{}, errors.New("语音服务地址无效")
	}
	request.Header.Set("Content-Type", "application/json")
	if profile.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+profile.APIKey)
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return TTSAudio{}, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return TTSAudio{}, errors.New("语音合成超时，请重试")
		}
		return TTSAudio{}, errors.New("无法连接语音服务，请检查服务地址和运行状态")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return TTSAudio{}, ttsStatusError(response.StatusCode)
	}
	audioBytes, err := io.ReadAll(io.LimitReader(response.Body, maxTTSAudioBytes+1))
	if err != nil {
		return TTSAudio{}, errors.New("无法读取语音响应")
	}
	if len(audioBytes) == 0 {
		return TTSAudio{}, errors.New("语音服务未返回音频内容")
	}
	if len(audioBytes) > maxTTSAudioBytes {
		return TTSAudio{}, errors.New("合成音频过大，请缩短朗读文本")
	}
	mimeType := defaultMIME
	if contentType := strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]); strings.HasPrefix(strings.ToLower(contentType), "audio/") {
		mimeType = strings.ToLower(contentType)
	}
	return TTSAudio{
		MimeType:   mimeType,
		DataBase64: base64.StdEncoding.EncodeToString(audioBytes),
	}, nil
}
