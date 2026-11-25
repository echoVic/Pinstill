package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// TranslationUpdate is the polling payload for a running translation.
type TranslationUpdate struct {
	Text  string `json:"text"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
}

type translationStream struct {
	mu       sync.Mutex
	text     strings.Builder
	done     bool
	err      string
	finished time.Time
	cancel   context.CancelFunc
}

func newTranslationToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// StartTranslation begins a streaming translation and returns the token the
// page polls with PollTranslation. Errors that would fail before any request
// are returned here; later failures arrive in the update payload.
func (service *ModelService) StartTranslation(ctx context.Context, revision, text, target, source, style string, glossary []GlossaryEntry) (string, error) {
	return service.startTranslationFor(ctx, mygo.CallerWindow(ctx), revision, text, target, source, style, glossary)
}

func (service *ModelService) startTranslationFor(ctx context.Context, caller *mygo.Window, revision, text, target, source, style string, glossary []GlossaryEntry) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if service.app != nil && !service.app.isTranslateWindow(caller) {
		if _, err := service.app.recognitionGenerationFor(ctx, caller); err != nil {
			return "", err
		}
	}
	profile, err := service.profileForRevision(revision)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errors.New("没有可翻译的文字")
	}
	if _, err := translationPromptForText(text, target, source, style, glossary); err != nil {
		return "", err
	}
	token, err := newTranslationToken()
	if err != nil {
		return "", errors.New("无法开始翻译")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	stream := &translationStream{cancel: cancel}
	service.streamsMu.Lock()
	if service.streams == nil {
		service.streams = make(map[string]*translationStream)
	}
	service.retireTranslationsLocked(time.Now().Add(-2 * time.Minute))
	service.streams[token] = stream
	service.streamsMu.Unlock()
	go func() {
		_, runErr := translateWithModelStream(ctx, modelHTTPClient, profile, text, target, source, style, glossary, func(delta string) {
			stream.mu.Lock()
			stream.text.WriteString(delta)
			stream.mu.Unlock()
		})
		stream.mu.Lock()
		stream.done = true
		stream.finished = time.Now()
		if runErr != nil {
			stream.err = translationErrorMessage(runErr)
		}
		stream.mu.Unlock()
		cancel()
	}()
	return token, nil
}

func translationErrorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "翻译超时，请重试"
	}
	if errors.Is(err, context.Canceled) {
		return "翻译已取消"
	}
	return err.Error()
}

// PollTranslation reports the translation so far. The page polls until Done.
func (service *ModelService) PollTranslation(token string) (TranslationUpdate, error) {
	service.streamsMu.Lock()
	stream := service.streams[token]
	service.streamsMu.Unlock()
	if stream == nil {
		return TranslationUpdate{}, errors.New("翻译已结束")
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return TranslationUpdate{Text: stream.text.String(), Done: stream.done, Error: stream.err}, nil
}

// CancelTranslation stops a running translation, e.g. when the page is closed.
func (service *ModelService) CancelTranslation(token string) error {
	service.streamsMu.Lock()
	stream := service.streams[token]
	service.streamsMu.Unlock()
	if stream != nil {
		stream.cancel()
	}
	return nil
}

// retireTranslationsLocked drops finished translations older than cutoff.
func (service *ModelService) retireTranslationsLocked(cutoff time.Time) {
	for token, stream := range service.streams {
		stream.mu.Lock()
		stale := stream.done && !stream.finished.IsZero() && stream.finished.Before(cutoff)
		stream.mu.Unlock()
		if stale {
			delete(service.streams, token)
		}
	}
}