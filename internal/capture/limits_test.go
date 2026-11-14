package capture

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/echoVic/Pinstill/internal/imagebudget"
)

func TestReadCaptureFileChecksBeforeRead(t *testing.T) {
	name := filepath.Join(t.TempDir(), "capture.png")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(imagebudget.MaxEncodedBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := readCaptureFile(name); err == nil || !strings.Contains(err.Error(), "byte") {
		t.Fatalf("oversize file accepted: %v", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	header := bytes.Clone(buf.Bytes()[:33])
	binary.BigEndian.PutUint32(header[16:], 8192)
	binary.BigEndian.PutUint32(header[20:], 4097)
	binary.BigEndian.PutUint32(header[29:], crc32.ChecksumIEEE(header[12:29]))
	if err := os.WriteFile(name, header, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCaptureFile(name); err == nil || !strings.Contains(err.Error(), "pixel") {
		t.Fatalf("oversize header accepted: %v", err)
	}
	if err := os.WriteFile(name, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readCaptureFile(name); err != nil || !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("valid file = %d bytes, %v", len(data), err)
	}
}

func TestCaptureDelayIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		sleepBeforeCapture(CaptureOptions{DelayMillis: 60_000})
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("delay = %v, want at most 10s", elapsed)
		}
	})
}

func TestCaptureCommandTimeout(t *testing.T) {
	t.Setenv("PINSTILL_TEST_CAPTURE_COMMAND", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runCaptureCommand(ctx, exe, "-test.run=^TestCaptureCommandHelper$"); err == nil {
		t.Fatal("command succeeded past deadline")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout returned after %v", elapsed)
	}
}

func TestCaptureCommandHelper(t *testing.T) {
	if os.Getenv("PINSTILL_TEST_CAPTURE_COMMAND") != "1" {
		return
	}
	time.Sleep(10 * time.Second)
}

func TestCursorPositionUsesHotspotAndCaptureOrigin(t *testing.T) {
	x, y, ok := cursorDrawPosition(-90, -40, 4, 6, -100, -50)
	if !ok || x != 6 || y != 4 {
		t.Fatalf("cursor offset = %d,%d,%v", x, y, ok)
	}
	if _, _, ok := cursorDrawPosition(-2147483648, 0, 0, 0, 2147483647, 0); ok {
		t.Fatal("overflowed cursor offset accepted")
	}
}
