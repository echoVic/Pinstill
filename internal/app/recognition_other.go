//go:build !darwin

package app

import (
	"context"
	"errors"
)

func recognizeWithVision(context.Context, string, []string, []byte) ([]string, error) {
	return nil, errors.New("截图识别目前支持 macOS")
}
