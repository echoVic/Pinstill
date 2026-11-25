package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/echoVic/Pinstill/internal/imagebudget"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

var (
	recognitionMu           sync.Mutex
	errRecognitionSelection = errors.New("没有可识别的截图选区")
)

type RecognitionResult struct {
	Text  string            `json:"text,omitempty"`
	Codes []RecognitionCode `json:"codes,omitempty"`
}

type RecognitionCode struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

func (app *App) isMainWindow(caller *mygo.Window) bool {
	if app == nil || caller == nil {
		return false
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	return !app.quitting && caller == app.main
}

func (app *App) isTranslateWindow(caller *mygo.Window) bool {
	if app == nil || caller == nil {
		return false
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	return !app.quitting && caller == app.translate
}

func (app *App) recognitionGeneration(ctx context.Context) (uint64, error) {
	return app.recognitionGenerationFor(ctx, mygo.CallerWindow(ctx))
}

func (app *App) recognitionGenerationFor(ctx context.Context, caller *mygo.Window) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if app == nil {
		return 0, errRecognitionSelection
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if caller == nil || caller != app.overlay || !app.overlayCommitted || !app.capturing || app.quitting || app.scroll != nil || app.overlayPreviewID == "" {
		return 0, errRecognitionSelection
	}
	return app.captureGeneration, nil
}

func (app *App) authorizeRecognizedCopy(ctx context.Context, caller *mygo.Window) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if app.isTranslateWindow(caller) {
		return nil
	}
	_, err := app.recognitionGenerationFor(ctx, caller)
	return err
}

func (service *CaptureService) RecognizeSelection(ctx context.Context, kind, language, dataBase64 string) (RecognitionResult, error) {
	app := service.app
	generation, err := app.recognitionGeneration(ctx)
	if err != nil {
		return RecognitionResult{}, err
	}
	if !recognitionMu.TryLock() {
		return RecognitionResult{}, errors.New("上一次识别正在结束，请重试")
	}
	defer recognitionMu.Unlock()
	data, err := decodeImageBase64(dataBase64)
	if err != nil {
		return RecognitionResult{}, err
	}
	result, err := recognizePNG(ctx, kind, language, data)
	if err != nil {
		return RecognitionResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RecognitionResult{}, err
	}
	if !app.selectionIsCurrent(generation) {
		return RecognitionResult{}, errRecognitionSelection
	}
	return result, nil
}

func (service *CaptureService) RecognizeTranslateImage(ctx context.Context, language, dataBase64 string) (string, error) {
	return service.recognizeTranslateImageFor(ctx, mygo.CallerWindow(ctx), language, dataBase64)
}

func (service *CaptureService) recognizeTranslateImageFor(ctx context.Context, caller *mygo.Window, language, dataBase64 string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !service.app.isTranslateWindow(caller) {
		return "", errRecognitionSelection
	}
	if !recognitionMu.TryLock() {
		return "", errors.New("上一次识别正在结束，请重试")
	}
	defer recognitionMu.Unlock()
	data, err := decodeImageBase64(dataBase64)
	if err != nil {
		return "", err
	}
	result, err := recognizePNG(ctx, "ocr", language, data)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

func recognizePNG(ctx context.Context, kind, language string, data []byte) (RecognitionResult, error) {
	if err := ctx.Err(); err != nil {
		return RecognitionResult{}, err
	}
	if kind != "ocr" && kind != "qr" {
		return RecognitionResult{}, errors.New("未知的识别类型")
	}
	if len(data) > imagebudget.MaxEncodedBytes {
		return RecognitionResult{}, errors.New("截图 PNG 超过 64 MiB 限制")
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return RecognitionResult{}, fmt.Errorf("截图 PNG 无效: %w", err)
	}
	if imagebudget.CheckDimensions(config.Width, config.Height) != nil {
		return RecognitionResult{}, errors.New("截图尺寸超出识别上限")
	}
	values, err := recognizeWithVision(ctx, kind, recognitionLanguages(language), data)
	if err != nil {
		return RecognitionResult{}, err
	}
	if kind == "ocr" {
		return RecognitionResult{Text: strings.Join(values, "\n")}, nil
	}
	result := RecognitionResult{Codes: make([]RecognitionCode, 0, len(values))}
	for _, value := range values {
		result.Codes = append(result.Codes, RecognitionCode{Text: value, URL: recognizedWebURL(value)})
	}
	return result, nil
}

func recognitionLanguages(preference string) []string {
	switch preference {
	case "简体中文":
		return []string{"zh-Hans"}
	case "English":
		return []string{"en-US"}
	case "日本語":
		return []string{"ja-JP"}
	default:
		return []string{"zh-Hans", "en-US"}
	}
}

func recognizedWebURL(value string) string {
	if strings.Contains(value, "\\") || strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character)
	}) >= 0 {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	return value
}

func (service *CaptureService) CopyRecognizedText(ctx context.Context, text string) error {
	if err := service.app.authorizeRecognizedCopy(ctx, mygo.CallerWindow(ctx)); err != nil {
		return err
	}
	if text == "" {
		return errors.New("没有可复制的文字")
	}
	return mygo.Clipboard.Write(transfer.TextData(text))
}

func (service *CaptureService) OpenRecognizedLink(ctx context.Context, value string) error {
	if recognizedWebURL(value) == "" {
		return errors.New("只能打开有效的 HTTP 或 HTTPS 网页链接")
	}
	app := service.app
	generation, err := app.recognitionGeneration(ctx)
	if err != nil {
		return err
	}
	app.runOverlayUI(func() {
		if err = ctx.Err(); err != nil {
			return
		}
		if !app.selectionIsCurrent(generation) {
			err = errRecognitionSelection
			return
		}
		if err = mygo.Shell.OpenExternal(value); err == nil {
			app.endRegion(generation, false)
		}
	})
	return err
}
