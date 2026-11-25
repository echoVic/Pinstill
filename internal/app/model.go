package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/egoist/mygo"
)

const (
	modelSettingsKey     = "recognition-model"
	modelIndexKey        = "recognition-model-index"
	modelPresetKeyPrefix = "recognition-model:"
	maxModelPresets      = 8
)

type ModelInput struct {
	Mode    string `json:"mode"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
}

type ModelPreset struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Mode      string `json:"mode"`
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"hasApiKey"`
	Revision  string `json:"revision"`
}

type ModelPresetInput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	APIKey  string `json:"apiKey"`
}

type GlossaryEntry struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

type ModelSettings struct {
	Mode      string        `json:"mode"`
	BaseURL   string        `json:"baseUrl"`
	Model     string        `json:"model"`
	HasAPIKey bool          `json:"hasApiKey"`
	Revision  string        `json:"revision"`
	ActiveID  string        `json:"activeId,omitempty"`
	Presets   []ModelPreset `json:"presets,omitempty"`
}

type modelProfile struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	ModelInput
	Revision string `json:"revision"`
}

type modelIndex struct {
	ActiveID string   `json:"activeId"`
	Order    []string `json:"order"`
}

func (profile modelProfile) preset() ModelPreset {
	return ModelPreset{
		ID: profile.ID, Name: profile.Name, Mode: profile.Mode,
		BaseURL: profile.BaseURL, Model: profile.Model,
		HasAPIKey: profile.APIKey != "", Revision: profile.Revision,
	}
}

func (profile modelProfile) settings() ModelSettings {
	return ModelSettings{
		Mode: profile.Mode, BaseURL: profile.BaseURL, Model: profile.Model,
		HasAPIKey: profile.APIKey != "", Revision: profile.Revision,
		ActiveID: profile.ID,
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

	streamsMu sync.Mutex
	streams   map[string]*translationStream
}

func normalizePresetName(name string, input ModelInput) string {
	cleaned := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if cleaned == "" {
		cleaned = strings.TrimSpace(input.Model)
	}
	if cleaned == "" {
		if endpoint, err := url.Parse(input.BaseURL); err == nil && endpoint.Hostname() != "" {
			cleaned = endpoint.Hostname()
		} else {
			cleaned = "默认模型"
		}
	}
	runes := []rune(cleaned)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return cleaned
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
			return ModelInput{}, errors.New("本地模型需要本机回环地址，例如 http://127.0.0.1:11434")
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

func (service *ModelService) deleteKey(key string) error {
	err := service.secrets.Delete(key)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil
	}
	return err
}

func (service *ModelService) loadProfileKey(key string) (modelProfile, error) {
	data, err := service.secrets.Get(key)
	if err != nil {
		return modelProfile{}, err
	}
	var profile modelProfile
	if json.Unmarshal(data, &profile) != nil || profile.Revision == "" {
		return modelProfile{}, errors.New("模型配置已损坏，请重新保存配置")
	}
	profile.ModelInput, err = normalizeModelInput(profile.ModelInput)
	return profile, err
}

func (service *ModelService) loadCatalog() (string, []modelProfile, bool, error) {
	indexBytes, err := service.secrets.Get(modelIndexKey)
	if err == nil {
		var index modelIndex
		if json.Unmarshal(indexBytes, &index) != nil {
			return "", nil, false, errors.New("模型预设索引已损坏，请重新保存配置")
		}
		profiles := make([]modelProfile, 0, len(index.Order))
		for _, id := range index.Order {
			if id == "" {
				continue
			}
			profile, loadErr := service.loadProfileKey(modelPresetKeyPrefix + id)
			if errors.Is(loadErr, mygo.ErrSecretNotFound) {
				continue
			}
			if loadErr != nil {
				return "", nil, false, loadErr
			}
			profile.ID = id
			profile.Name = normalizePresetName(profile.Name, profile.ModelInput)
			profiles = append(profiles, profile)
		}
		activeID := ""
		for _, profile := range profiles {
			if profile.ID == index.ActiveID {
				activeID = index.ActiveID
				break
			}
		}
		return activeID, profiles, false, nil
	}
	if !errors.Is(err, mygo.ErrSecretNotFound) {
		return "", nil, false, errors.New("无法从系统钥匙串读取模型配置")
	}
	legacyProfile, err := service.loadProfileKey(modelSettingsKey)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return "", nil, false, nil
	}
	if err != nil {
		if errors.Is(err, mygo.ErrSecretNotFound) {
			return "", nil, false, nil
		}
		return "", nil, false, errors.New("无法从系统钥匙串读取模型配置")
	}
	legacyProfile.ID = "default"
	legacyProfile.Name = normalizePresetName(legacyProfile.Name, legacyProfile.ModelInput)
	return legacyProfile.ID, []modelProfile{legacyProfile}, true, nil
}

func buildModelSettings(activeID string, profiles []modelProfile) ModelSettings {
	presets := make([]ModelPreset, 0, len(profiles))
	settings := ModelSettings{Mode: "off"}
	for _, profile := range profiles {
		presets = append(presets, profile.preset())
		if activeID != "" && profile.ID == activeID {
			settings = profile.settings()
		}
	}
	if len(presets) > 0 {
		settings.Presets = presets
	}
	return settings
}

func (service *ModelService) saveIndex(activeID string, profiles []modelProfile) error {
	order := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		order = append(order, profile.ID)
	}
	data, err := json.Marshal(modelIndex{ActiveID: activeID, Order: order})
	if err != nil || len(data) > mygo.MaxSecretSize {
		return errors.New("模型预设数量过多，无法保存")
	}
	if err := service.secrets.Set(modelIndexKey, data); err != nil {
		return errors.New("无法将模型配置保存到系统钥匙串")
	}
	return nil
}

func (service *ModelService) copyLegacyPresetsLocked(profiles []modelProfile) error {
	for _, profile := range profiles {
		data, err := json.Marshal(profile)
		if err != nil || len(data) > mygo.MaxSecretSize {
			return errors.New("模型配置过长，请检查服务地址、模型名和 API Key")
		}
		if err := service.secrets.Set(modelPresetKeyPrefix+profile.ID, data); err != nil {
			return errors.New("无法将模型配置保存到系统钥匙串")
		}
	}
	return nil
}

// The index is the commit point: before it exists, the legacy record remains
// authoritative. Never remove that fallback before the index is saved.
func (service *ModelService) commitModelIndex(activeID string, profiles []modelProfile, legacy bool) error {
	if err := service.saveIndex(activeID, profiles); err != nil {
		return err
	}
	if legacy {
		if err := service.deleteKey(modelSettingsKey); err != nil {
			log.Print("pinstill: could not remove the superseded model configuration from Keychain")
		}
	}
	return nil
}

func newRandomHex(byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func (service *ModelService) load() (modelProfile, error) {
	activeID, profiles, _, err := service.loadCatalog()
	if err != nil {
		return modelProfile{}, err
	}
	for _, profile := range profiles {
		if activeID != "" && profile.ID == activeID {
			return profile, nil
		}
	}
	return modelProfile{ModelInput: ModelInput{Mode: "off"}}, nil
}

func (service *ModelService) save(input ModelInput) (ModelSettings, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	normalized, err := normalizeModelInput(input)
	if err != nil {
		return ModelSettings{}, err
	}
	if normalized.Mode == "off" {
		_, profiles, _, loadErr := service.loadCatalog()
		if loadErr == nil {
			for _, profile := range profiles {
				if err := service.deleteKey(modelPresetKeyPrefix + profile.ID); err != nil {
					return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
				}
			}
		}
		if err := service.deleteKey(modelSettingsKey); err != nil {
			return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
		}
		if err := service.deleteKey(modelIndexKey); err != nil {
			return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
		}
		return ModelSettings{Mode: "off"}, nil
	}
	activeID, profiles, _, err := service.loadCatalog()
	if err != nil {
		return ModelSettings{}, err
	}
	targetID := activeID
	targetName := ""
	for _, profile := range profiles {
		if profile.ID == targetID {
			targetName = profile.Name
			break
		}
	}
	return service.savePresetLocked(ModelPresetInput{
		ID:      targetID,
		Name:    targetName,
		Mode:    normalized.Mode,
		BaseURL: normalized.BaseURL,
		Model:   normalized.Model,
		APIKey:  normalized.APIKey,
	})
}

func (service *ModelService) savePresetLocked(input ModelPresetInput) (ModelSettings, error) {
	if input.Mode == "off" {
		return ModelSettings{}, errors.New("请选择本地模型或云端 API")
	}
	normalized, err := normalizeModelInput(ModelInput{
		Mode:    input.Mode,
		BaseURL: input.BaseURL,
		Model:   input.Model,
		APIKey:  input.APIKey,
	})
	if err != nil {
		return ModelSettings{}, err
	}
	_, profiles, legacy, err := service.loadCatalog()
	if err != nil {
		return ModelSettings{}, err
	}
	id := strings.TrimSpace(input.ID)
	existingIndex := -1
	if id != "" {
		existingIndex = slices.IndexFunc(profiles, func(profile modelProfile) bool {
			return profile.ID == id
		})
		if existingIndex < 0 {
			return ModelSettings{}, errors.New("找不到要更新的模型预设")
		}
		previous := profiles[existingIndex]
		if normalized.APIKey == "" && previous.Mode == normalized.Mode && previous.BaseURL == normalized.BaseURL {
			normalized.APIKey = previous.APIKey
		}
	} else {
		if len(profiles) >= maxModelPresets {
			return ModelSettings{}, errors.New("最多可保存 8 个模型预设")
		}
		suffix, err := newRandomHex(8)
		if err != nil {
			return ModelSettings{}, errors.New("无法生成模型预设标识")
		}
		id = "preset-" + suffix
	}
	if normalized.Mode == "cloud" && normalized.APIKey == "" {
		return ModelSettings{}, errors.New("请输入该云端服务的 API Key")
	}
	revision, err := newRandomHex(16)
	if err != nil {
		return ModelSettings{}, errors.New("无法生成模型配置标识")
	}
	profile := modelProfile{
		ID:         id,
		Name:       normalizePresetName(input.Name, normalized),
		ModelInput: normalized,
		Revision:   revision,
	}
	data, err := json.Marshal(profile)
	if err != nil || len(data) > mygo.MaxSecretSize {
		return ModelSettings{}, errors.New("模型配置过长，请检查服务地址、模型名和 API Key")
	}
	if legacy {
		if err := service.copyLegacyPresetsLocked(profiles); err != nil {
			return ModelSettings{}, err
		}
	}
	if err := service.secrets.Set(modelPresetKeyPrefix+id, data); err != nil {
		return ModelSettings{}, errors.New("无法将模型配置保存到系统钥匙串")
	}
	if existingIndex >= 0 {
		profiles[existingIndex] = profile
	} else {
		profiles = append(profiles, profile)
	}
	if err := service.commitModelIndex(id, profiles, legacy); err != nil {
		return ModelSettings{}, err
	}
	return buildModelSettings(id, profiles), nil
}

func (service *ModelService) settingsFor(ctx context.Context, caller *mygo.Window) (ModelSettings, error) {
	if err := ctx.Err(); err != nil {
		return ModelSettings{}, err
	}
	if !service.app.isMainWindow(caller) && !service.app.isTranslateWindow(caller) {
		if _, err := service.app.recognitionGenerationFor(ctx, caller); err != nil {
			return ModelSettings{}, err
		}
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	activeID, profiles, _, err := service.loadCatalog()
	if err != nil {
		return ModelSettings{}, err
	}
	return buildModelSettings(activeID, profiles), nil
}

func (service *ModelService) Settings(ctx context.Context) (ModelSettings, error) {
	return service.settingsFor(ctx, mygo.CallerWindow(ctx))
}

func (service *ModelService) saveSettingsFor(ctx context.Context, caller *mygo.Window, input ModelInput) (ModelSettings, error) {
	if err := ctx.Err(); err != nil {
		return ModelSettings{}, err
	}
	if !service.app.isMainWindow(caller) {
		return ModelSettings{}, errors.New("请在偏好设置中保存模型配置")
	}
	return service.save(input)
}

func (service *ModelService) SaveSettings(ctx context.Context, input ModelInput) (ModelSettings, error) {
	return service.saveSettingsFor(ctx, mygo.CallerWindow(ctx), input)
}

func (service *ModelService) savePresetFor(ctx context.Context, caller *mygo.Window, input ModelPresetInput) (ModelSettings, error) {
	if err := ctx.Err(); err != nil {
		return ModelSettings{}, err
	}
	if !service.app.isMainWindow(caller) {
		return ModelSettings{}, errors.New("请在偏好设置中保存模型配置")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.savePresetLocked(input)
}

func (service *ModelService) SavePreset(ctx context.Context, input ModelPresetInput) (ModelSettings, error) {
	return service.savePresetFor(ctx, mygo.CallerWindow(ctx), input)
}

func (service *ModelService) deletePresetFor(ctx context.Context, caller *mygo.Window, id string) (ModelSettings, error) {
	if err := ctx.Err(); err != nil {
		return ModelSettings{}, err
	}
	if !service.app.isMainWindow(caller) {
		return ModelSettings{}, errors.New("请在偏好设置中管理模型预设")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ModelSettings{}, errors.New("找不到要删除的模型预设")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	activeID, profiles, _, err := service.loadCatalog()
	if err != nil {
		return ModelSettings{}, err
	}
	index := slices.IndexFunc(profiles, func(profile modelProfile) bool {
		return profile.ID == id
	})
	if index < 0 {
		return ModelSettings{}, errors.New("找不到要删除的模型预设")
	}
	if len(profiles) == 1 {
		if err := service.deleteKey(modelSettingsKey); err != nil {
			return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
		}
	}
	if err := service.deleteKey(modelPresetKeyPrefix + id); err != nil {
		return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
	}
	remaining := append(profiles[:index], profiles[index+1:]...)
	if len(remaining) == 0 {
		if err := service.deleteKey(modelIndexKey); err != nil {
			return ModelSettings{}, errors.New("无法从系统钥匙串移除模型配置")
		}
		return ModelSettings{Mode: "off"}, nil
	}
	if activeID == id {
		activeID = remaining[0].ID
	}
	if err := service.saveIndex(activeID, remaining); err != nil {
		return ModelSettings{}, err
	}
	return buildModelSettings(activeID, remaining), nil
}

func (service *ModelService) DeletePreset(ctx context.Context, id string) (ModelSettings, error) {
	return service.deletePresetFor(ctx, mygo.CallerWindow(ctx), id)
}

func (service *ModelService) selectPresetFor(ctx context.Context, caller *mygo.Window, id string) (ModelSettings, error) {
	if err := ctx.Err(); err != nil {
		return ModelSettings{}, err
	}
	isMain := service.app.isMainWindow(caller)
	isTranslate := service.app.isTranslateWindow(caller)
	if !isMain && !isTranslate {
		return ModelSettings{}, errRecognitionSelection
	}
	id = strings.TrimSpace(id)
	if id == "" && !isMain {
		return ModelSettings{}, errors.New("请在偏好设置中关闭模型服务")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	_, profiles, legacy, err := service.loadCatalog()
	if err != nil {
		return ModelSettings{}, err
	}
	if id != "" && !slices.ContainsFunc(profiles, func(profile modelProfile) bool { return profile.ID == id }) {
		return ModelSettings{}, errors.New("找不到该模型预设")
	}
	if legacy {
		if err := service.copyLegacyPresetsLocked(profiles); err != nil {
			return ModelSettings{}, err
		}
	}
	if err := service.commitModelIndex(id, profiles, legacy); err != nil {
		return ModelSettings{}, err
	}
	return buildModelSettings(id, profiles), nil
}

func (service *ModelService) SelectPreset(ctx context.Context, id string) (ModelSettings, error) {
	return service.selectPresetFor(ctx, mygo.CallerWindow(ctx), id)
}

func (service *ModelService) testConnectionFor(ctx context.Context, caller *mygo.Window, input ModelPresetInput) (string, error) {
	return service.testConnectionWithClient(ctx, caller, modelHTTPClient, input)
}

func (service *ModelService) testConnectionWithClient(ctx context.Context, caller *mygo.Window, client *http.Client, input ModelPresetInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !service.app.isMainWindow(caller) {
		return "", errors.New("请在偏好设置中测试模型连接")
	}
	if input.Mode == "off" {
		return "", errors.New("请先选择本地模型或云端 API")
	}
	normalized, err := normalizeModelInput(ModelInput{
		Mode:    input.Mode,
		BaseURL: input.BaseURL,
		Model:   input.Model,
		APIKey:  input.APIKey,
	})
	if err != nil {
		return "", err
	}
	if normalized.APIKey == "" {
		service.mu.Lock()
		activeID, profiles, _, loadErr := service.loadCatalog()
		service.mu.Unlock()
		if loadErr != nil {
			return "", loadErr
		}
		targetID := strings.TrimSpace(input.ID)
		if targetID == "" {
			targetID = activeID
		}
		for _, profile := range profiles {
			if profile.ID == targetID && profile.Mode == normalized.Mode && profile.BaseURL == normalized.BaseURL {
				normalized.APIKey = profile.APIKey
				break
			}
		}
	}
	if normalized.Mode == "cloud" && normalized.APIKey == "" {
		return "", errors.New("请输入该云端服务的 API Key")
	}
	return testModelConnection(ctx, client, modelProfile{ModelInput: normalized})
}

func (service *ModelService) TestConnection(ctx context.Context, input ModelPresetInput) (string, error) {
	return service.testConnectionFor(ctx, mygo.CallerWindow(ctx), input)
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

func (service *ModelService) TranslateText(ctx context.Context, revision, text, target, source, style string, glossary []GlossaryEntry) (string, error) {
	return service.runForSelection(ctx, revision, func(profile modelProfile) (string, error) {
		return translateWithModel(ctx, modelHTTPClient, profile, text, target, source, style, glossary)
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
