package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/egoist/mygo"
)

const modelSettingsKey = "recognition-model"

type ModelInput struct {
	Mode    string `json:"mode"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
}

type ModelSettings struct {
	Mode      string `json:"mode"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"hasApiKey"`
	Revision  string `json:"revision"`
}

type modelProfile struct {
	ModelInput
	Revision string `json:"revision"`
}

func (profile modelProfile) settings() ModelSettings {
	return ModelSettings{
		Mode: profile.Mode, BaseURL: profile.BaseURL, Model: profile.Model,
		HasAPIKey: profile.APIKey != "", Revision: profile.Revision,
	}
}

type modelSecretStore interface {
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
}

type ModelService struct {
	app     *App
	secrets modelSecretStore
	mu      sync.Mutex
}

func normalizeModelInput(input ModelInput) (ModelInput, error) {
	if input.Mode == "off" {
		return ModelInput{Mode: "off"}, nil
	}
	if input.Mode != "local" && input.Mode != "cloud" {
		return ModelInput{}, errors.New("请选择模型服务类型")
	}
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Model = strings.TrimSpace(input.Model)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.Model == "" || len(input.Model) > 200 || strings.IndexFunc(input.Model, unicode.IsControl) >= 0 {
		return ModelInput{}, errors.New("请输入有效的模型名")
	}
	if len(input.APIKey) > 1024 || strings.IndexFunc(input.APIKey, func(character rune) bool {
		return character < 33 || character > 126
	}) >= 0 {
		return ModelInput{}, errors.New("API Key 格式无效")
	}
	if len(input.BaseURL) > 512 || strings.ContainsAny(input.BaseURL, "\\?#") || strings.IndexFunc(input.BaseURL, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
	}) >= 0 {
		return ModelInput{}, errors.New("请输入不带查询参数的服务地址")
	}
	endpoint, err := url.Parse(input.BaseURL)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawPath != "" ||
		(endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return ModelInput{}, errors.New("请输入有效的 HTTP 或 HTTPS 服务地址")
	}
	if input.Mode == "local" {
		host := endpoint.Hostname()
		if strings.EqualFold(host, "localhost") {
			port := endpoint.Port()
			endpoint.Host = "127.0.0.1"
			if port != "" {
				endpoint.Host = net.JoinHostPort("127.0.0.1", port)
			}
		} else if !net.ParseIP(host).IsLoopback() {
			return ModelInput{}, errors.New("本地模型需要本机回环地址，例如 http://127.0.0.1:11434/v1")
		}
	} else if endpoint.Scheme != "https" {
		return ModelInput{}, errors.New("云端模型需要 HTTPS 服务地址")
	}
	endpoint.Path = strings.TrimSuffix(strings.TrimRight(endpoint.Path, "/"), "/chat/completions")
	if endpoint.Path == "" {
		endpoint.Path = "/v1"
	}
	input.BaseURL = endpoint.String()
	return input, nil
}

func (service *ModelService) load() (modelProfile, error) {
	data, err := service.secrets.Get(modelSettingsKey)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return modelProfile{ModelInput: ModelInput{Mode: "off"}}, nil
	}
	if err != nil {
		return modelProfile{}, errors.New("无法从系统钥匙串读取模型配置")
	}
	var profile modelProfile
	if json.Unmarshal(data, &profile) != nil || profile.Revision == "" {
		return modelProfile{}, errors.New("模型配置已损坏，请重新保存配置")
	}
	profile.ModelInput, err = normalizeModelInput(profile.ModelInput)
	return profile, err
}

func (service *ModelService) save(input ModelInput) (ModelSettings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	input, err := normalizeModelInput(input)
	if err != nil {
		return ModelSettings{}, err
	}
	if input.Mode == "off" {
		if err := service.secrets.Delete(modelSettingsKey); err != nil {
			return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
		}
		return ModelSettings{Mode: "off"}, nil
	}
	if input.APIKey == "" {
		previous, err := service.load()
		if err != nil {
			return ModelSettings{}, err
		}
		if previous.Mode == input.Mode && previous.BaseURL == input.BaseURL {
			input.APIKey = previous.APIKey
		}
	}
	if input.Mode == "cloud" && input.APIKey == "" {
		return ModelSettings{}, errors.New("请输入该云端服务的 API Key")
	}
	var revision [16]byte
	if _, err := rand.Read(revision[:]); err != nil {
		return ModelSettings{}, errors.New("无法生成模型配置标识")
	}
	profile := modelProfile{ModelInput: input, Revision: hex.EncodeToString(revision[:])}
	data, err := json.Marshal(profile)
	if err != nil || len(data) > mygo.MaxSecretSize {
		return ModelSettings{}, errors.New("模型配置过长，请检查服务地址、模型名和 API Key")
	}
	if err := service.secrets.Set(modelSettingsKey, data); err != nil {
		return ModelSettings{}, errors.New("无法将模型配置保存到系统钥匙串")
	}
	return profile.settings(), nil
}

func (service *ModelService) calledFromMain(ctx context.Context) bool {
	caller := mygo.CallerWindow(ctx)
	return caller != nil && caller == service.app.main
}

func (service *ModelService) Settings(ctx context.Context) (ModelSettings, error) {
	if !service.calledFromMain(ctx) {
		if _, err := service.app.recognitionGeneration(ctx); err != nil {
			return ModelSettings{}, err
		}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	profile, err := service.load()
	return profile.settings(), err
}

func (service *ModelService) SaveSettings(ctx context.Context, input ModelInput) (ModelSettings, error) {
	if !service.calledFromMain(ctx) {
		return ModelSettings{}, errors.New("请在偏好设置中保存模型配置")
	}
	return service.save(input)
}

func (service *ModelService) profileForRevision(revision string) (modelProfile, error) {
	profile, err := service.load()
	if err != nil {
		return modelProfile{}, err
	}
	if profile.Mode == "off" {
		return modelProfile{}, errors.New("请先在偏好设置 → 识别与翻译中配置模型服务")
	}
	if revision == "" || profile.Revision != revision {
		return modelProfile{}, errors.New("模型配置已更改，请重新发起操作")
	}
	return profile, nil
}

func (service *ModelService) TranslateText(ctx context.Context, revision, text, target string) (string, error) {
	return service.runForSelection(ctx, revision, func(profile modelProfile) (string, error) {
		return translateWithModel(ctx, modelHTTPClient, profile, text, target)
	})
}

func (service *ModelService) RecognizeImage(ctx context.Context, revision, imageBase64 string) (string, error) {
	return service.runForSelection(ctx, revision, func(profile modelProfile) (string, error) {
		return recognizeWithModel(ctx, modelHTTPClient, profile, imageBase64)
	})
}

func (service *ModelService) runForSelection(ctx context.Context, revision string, run func(modelProfile) (string, error)) (string, error) {
	generation, err := service.app.recognitionGeneration(ctx)
	if err != nil {
		return "", err
	}
	profile, err := service.profileForRevision(revision)
	if err != nil {
		return "", err
	}
	if !service.app.selectionIsCurrent(generation) {
		return "", errRecognitionSelection
	}
	value, err := run(profile)
	if err != nil {
		return "", err
	}
	if !service.app.selectionIsCurrent(generation) {
		return "", errRecognitionSelection
	}
	return value, ctx.Err()
}
