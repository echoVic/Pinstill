package capture

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"time"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

const captureTimeout = 10 * time.Second

func readCaptureFile(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() <= 0 || stat.Size() > imagebudget.MaxEncodedBytes {
		return nil, fmt.Errorf("capture: invalid screenshot file or encoded byte limit exceeded")
	}
	config, err := png.DecodeConfig(io.LimitReader(f, imagebudget.MaxEncodedBytes+1))
	if err != nil {
		return nil, err
	}
	if err := imagebudget.CheckDimensions(config.Width, config.Height); err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	// LimitReader also protects against growth after Stat.
	data, err := io.ReadAll(io.LimitReader(f, imagebudget.MaxEncodedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > imagebudget.MaxEncodedBytes {
		return nil, fmt.Errorf("capture: encoded byte limit exceeded")
	}
	return data, nil
}

func runCaptureCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	// Capture commands only need a short diagnostic; never retain arbitrary
	// child-process output while waiting for the deadline.
	out := limitedBuffer{limit: 16 << 10, discard: true}
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return out.Bytes(), err
}

func cursorDrawPosition(x, y int32, hotX, hotY uint32, captureX, captureY int) (int32, int32, bool) {
	dx, dy := int64(x)-int64(hotX)-int64(captureX), int64(y)-int64(hotY)-int64(captureY)
	if dx < math.MinInt32 || dx > math.MaxInt32 || dy < math.MinInt32 || dy > math.MaxInt32 {
		return 0, 0, false
	}
	return int32(dx), int32(dy), true
}

type limitedBuffer struct {
	bytes.Buffer
	limit   int
	discard bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if n > b.limit-b.Len() {
		if !b.discard {
			return 0, fmt.Errorf("capture: encoded byte limit exceeded")
		}
		p = p[:b.limit-b.Len()]
	}
	_, err := b.Buffer.Write(p)
	return n, err
}
