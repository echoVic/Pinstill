package app

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
	"time"
)

// gnSettings returns DefaultSettings with optional overrides.
func gnSettings(mutate func(*Settings)) Settings {
	settings := DefaultSettings()
	if mutate != nil {
		mutate(&settings)
	}
	return settings
}

func TestNormalizeSettingsDefaults(t *testing.T) {
	got := normalizeSettings(Settings{})
	want := Settings{
		AfterCapture:   "toolbar",
		Format:         "PNG",
		Quality:        10,
		FilenamePrefix: "Tupen",
		Folder:         "~/Pictures/Tupen",
		PinOpacity:     100,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeSettings(zero) = %+v, want %+v", got, want)
	}

	// The app's own defaults must already be normalized.
	if defaults := DefaultSettings(); !reflect.DeepEqual(normalizeSettings(defaults), defaults) {
		t.Fatalf("normalizeSettings(DefaultSettings()) = %+v, want %+v",
			normalizeSettings(defaults), defaults)
	}
}

func TestNormalizeSettingsKeysAndEnums(t *testing.T) {
	tests := []struct {
		name string
		in   Settings
		want Settings
	}{
		{
			name: "shortcut keys are trimmed",
			in:   gnSettings(func(s *Settings) { s.CaptureKey = "  capture  "; s.PinKey = " pin "; s.OcrKey = "\tocr\n" }),
			want: gnSettings(func(s *Settings) { s.CaptureKey = "capture"; s.PinKey = "pin"; s.OcrKey = "ocr" }),
		},
		{
			name: "afterCapture editor",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = "editor" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "editor" }),
		},
		{
			name: "afterCapture clipboard",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = "clipboard" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "clipboard" }),
		},
		{
			name: "afterCapture pin",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = "pin" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "pin" }),
		},
		{
			name: "afterCapture wrong case falls back to toolbar",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = "Clipboard" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "toolbar" }),
		},
		{
			name: "afterCapture unknown falls back to toolbar",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = "bogus" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "toolbar" }),
		},
		{
			name: "afterCapture empty falls back to toolbar",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = "" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "toolbar" }),
		},
		{
			name: "afterCapture padded value is not trimmed",
			in:   gnSettings(func(s *Settings) { s.AfterCapture = " clipboard" }),
			want: gnSettings(func(s *Settings) { s.AfterCapture = "toolbar" }),
		},
		{
			name: "format JPEG is kept",
			in:   gnSettings(func(s *Settings) { s.Format = "JPEG" }),
			want: gnSettings(func(s *Settings) { s.Format = "JPEG" }),
		},
		{
			name: "format lowercase jpeg becomes PNG",
			in:   gnSettings(func(s *Settings) { s.Format = "jpeg" }),
			want: gnSettings(func(s *Settings) { s.Format = "PNG" }),
		},
		{
			name: "format unknown becomes PNG",
			in:   gnSettings(func(s *Settings) { s.Format = "GIF" }),
			want: gnSettings(func(s *Settings) { s.Format = "PNG" }),
		},
		{
			name: "format empty becomes PNG",
			in:   gnSettings(func(s *Settings) { s.Format = "" }),
			want: gnSettings(func(s *Settings) { s.Format = "PNG" }),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSettings(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeSettings() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNormalizeSettingsNumbers(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Settings)
		check  func(*testing.T, Settings)
	}{
		{
			name:   "quality is clamped to 10..100",
			mutate: func(s *Settings) { s.Quality = -5 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 10) },
		},
		{
			name:   "quality zero is raised to 10",
			mutate: func(s *Settings) { s.Quality = 0 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 10) },
		},
		{
			name:   "quality 9 is raised to 10",
			mutate: func(s *Settings) { s.Quality = 9 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 10) },
		},
		{
			name:   "quality boundary 10 is kept",
			mutate: func(s *Settings) { s.Quality = 10 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 10) },
		},
		{
			name:   "quality 90 is kept",
			mutate: func(s *Settings) { s.Quality = 90 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 90) },
		},
		{
			name:   "quality boundary 100 is kept",
			mutate: func(s *Settings) { s.Quality = 100 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 100) },
		},
		{
			name:   "quality above 100 is clamped",
			mutate: func(s *Settings) { s.Quality = 1000 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "Quality", got.Quality, 100) },
		},
		{
			name:   "retention days negative becomes zero",
			mutate: func(s *Settings) { s.RetentionDays = -3 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "RetentionDays", got.RetentionDays, 0) },
		},
		{
			name:   "retention days positive is kept",
			mutate: func(s *Settings) { s.RetentionDays = 7 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "RetentionDays", got.RetentionDays, 7) },
		},
		{
			name:   "history limit negative becomes zero",
			mutate: func(s *Settings) { s.HistoryLimitMB = -1 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "HistoryLimitMB", got.HistoryLimitMB, 0) },
		},
		{
			name:   "history limit positive is kept",
			mutate: func(s *Settings) { s.HistoryLimitMB = 512 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "HistoryLimitMB", got.HistoryLimitMB, 512) },
		},
		{
			name:   "pin opacity zero falls back to 100",
			mutate: func(s *Settings) { s.PinOpacity = 0 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "PinOpacity", got.PinOpacity, 100) },
		},
		{
			name:   "pin opacity negative falls back to 100",
			mutate: func(s *Settings) { s.PinOpacity = -20 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "PinOpacity", got.PinOpacity, 100) },
		},
		{
			name:   "pin opacity 1 is raised to 30",
			mutate: func(s *Settings) { s.PinOpacity = 1 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "PinOpacity", got.PinOpacity, 30) },
		},
		{
			name:   "pin opacity 29 is raised to 30",
			mutate: func(s *Settings) { s.PinOpacity = 29 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "PinOpacity", got.PinOpacity, 30) },
		},
		{
			name:   "pin opacity 45 is kept",
			mutate: func(s *Settings) { s.PinOpacity = 45 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "PinOpacity", got.PinOpacity, 45) },
		},
		{
			name:   "pin opacity above 100 is clamped",
			mutate: func(s *Settings) { s.PinOpacity = 150 },
			check:  func(t *testing.T, got Settings) { gnWant(t, "PinOpacity", got.PinOpacity, 100) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, normalizeSettings(gnSettings(tt.mutate)))
		})
	}
}

// gnWant fails the test when got differs from want.
func gnWant[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
}

func TestNormalizeSettingsFilenamesAndFolders(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*Settings)
		wantPrefix string
		wantFolder string
	}{
		{
			name:       "empty prefix and folder get defaults",
			mutate:     func(s *Settings) { s.FilenamePrefix = ""; s.Folder = "" },
			wantPrefix: "Tupen",
			wantFolder: "~/Pictures/Tupen",
		},
		{
			name:       "whitespace prefix and folder get defaults",
			mutate:     func(s *Settings) { s.FilenamePrefix = "  "; s.Folder = "\t " },
			wantPrefix: "Tupen",
			wantFolder: "~/Pictures/Tupen",
		},
		{
			name:       "custom values are kept",
			mutate:     func(s *Settings) { s.FilenamePrefix = "My Shots"; s.Folder = "~/Pictures/Snips" },
			wantPrefix: "My Shots",
			wantFolder: "~/Pictures/Snips",
		},
		{
			name:       "padded values are kept verbatim",
			mutate:     func(s *Settings) { s.FilenamePrefix = "  My Shots  "; s.Folder = " /tmp/snips " },
			wantPrefix: "  My Shots  ",
			wantFolder: " /tmp/snips ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSettings(gnSettings(tt.mutate))
			gnWant(t, "FilenamePrefix", got.FilenamePrefix, tt.wantPrefix)
			gnWant(t, "Folder", got.Folder, tt.wantFolder)
		})
	}
}

func TestNormalizeSettingsPreservesUnrelatedFields(t *testing.T) {
	in := gnSettings(func(s *Settings) {
		s.IncludeCursor = true
		s.Sound = false
		s.LaunchAtLogin = true
		s.KeepHistory = false
		s.Tooltip = false
		s.PinBorder = false
		s.IgnoredSettings = []string{"unknownKey"}
	})
	got := normalizeSettings(in)
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("normalizeSettings() changed unrelated fields: got %+v, want %+v", got, in)
	}
}

func TestNormalizeSettingsIdempotent(t *testing.T) {
	messy := Settings{
		CaptureKey:      "  CmdOrCtrl+Shift+4 ",
		PinKey:          " pin ",
		OcrKey:          " ",
		AfterCapture:    "nope",
		Format:          "gif",
		Quality:         -1,
		FilenamePrefix:  "",
		Folder:          "",
		RetentionDays:   -5,
		HistoryLimitMB:  -1,
		PinOpacity:      -3,
		IgnoredSettings: []string{"x"},
	}
	once := normalizeSettings(messy)
	twice := normalizeSettings(once)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("normalizeSettings is not idempotent: once %+v, twice %+v", once, twice)
	}
}

func TestNormalizeExport(t *testing.T) {
	pngData := gnPNGData(t, 4, 3)
	jpegData := gnJPEGData(t, 5, 4, 90)
	pngMagic := gnPNGMagic()

	tests := []struct {
		name       string
		raw        []byte
		settings   Settings
		wantExt    string
		sameBytes  bool
		wantErr    bool
		wantWidth  int
		wantHeight int
	}{
		{
			name:      "PNG stays PNG",
			raw:       pngData,
			settings:  Settings{Format: "PNG"},
			wantExt:   "png",
			sameBytes: true,
		},
		{
			name:       "PNG converts to JPEG",
			raw:        pngData,
			settings:   Settings{Format: "JPEG", Quality: 80},
			wantExt:    "jpg",
			wantWidth:  4,
			wantHeight: 3,
		},
		{
			name:      "JPEG stays JPEG",
			raw:       jpegData,
			settings:  Settings{Format: "JPEG", Quality: 80},
			wantExt:   "jpg",
			sameBytes: true,
		},
		{
			name:       "JPEG converts to PNG",
			raw:        jpegData,
			settings:   Settings{Format: "PNG"},
			wantExt:    "png",
			wantWidth:  5,
			wantHeight: 4,
		},
		{
			name:      "unknown format falls back to PNG",
			raw:       pngData,
			settings:  Settings{Format: "GIF"},
			wantExt:   "png",
			sameBytes: true,
		},
		{
			name:     "unsupported bytes fail",
			raw:      []byte("GIF89a definitely not supported"),
			settings: Settings{Format: "PNG"},
			wantErr:  true,
		},
		{
			name:     "empty bytes fail",
			raw:      nil,
			settings: Settings{Format: "PNG"},
			wantErr:  true,
		},
		{
			name:     "truncated JPEG fails to convert to PNG",
			raw:      []byte{0xFF, 0xD8, 0xFF, 0x00},
			settings: Settings{Format: "PNG"},
			wantErr:  true,
		},
		{
			// Same-format passthrough does not validate the body: a JPEG target
			// with JPEG-looking bytes is returned as-is.
			name:      "truncated JPEG passes through for JPEG target",
			raw:       []byte{0xFF, 0xD8, 0xFF, 0x00},
			settings:  Settings{Format: "JPEG", Quality: 90},
			wantExt:   "jpg",
			sameBytes: true,
		},
		{
			name:      "PNG magic only passes through unchanged for PNG target",
			raw:       pngMagic,
			settings:  Settings{Format: "PNG"},
			wantExt:   "png",
			sameBytes: true,
		},
		{
			name:     "PNG magic only fails when converting to JPEG",
			raw:      pngMagic,
			settings: Settings{Format: "JPEG", Quality: 90},
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ext, err := normalizeExport(tt.raw, tt.settings)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("normalizeExport() returned nil error, want error (out=%d bytes)", len(out))
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeExport() error = %v", err)
			}
			if ext != tt.wantExt {
				t.Fatalf("normalizeExport() ext = %q, want %q", ext, tt.wantExt)
			}
			if tt.sameBytes && !bytes.Equal(out, tt.raw) {
				t.Fatalf("normalizeExport() changed passthrough bytes")
			}
			if tt.wantWidth > 0 {
				width, height := gnImageConfig(t, out)
				if width != tt.wantWidth || height != tt.wantHeight {
					t.Fatalf("normalizeExport() image = %dx%d, want %dx%d", width, height, tt.wantWidth, tt.wantHeight)
				}
			}
			switch tt.wantExt {
			case "png":
				if !bytes.HasPrefix(out, pngMagic) {
					t.Fatalf("normalizeExport() output has no PNG signature: %x", out[:min(8, len(out))])
				}
			case "jpg":
				if !gnHasJPEGMagic(out) {
					t.Fatalf("normalizeExport() output has no JPEG magic: %x", out[:min(3, len(out))])
				}
			}
		})
	}
}

func TestDetectImageFormat(t *testing.T) {
	pngMagic := gnPNGMagic()
	tests := []struct {
		name    string
		raw     []byte
		want    string
		wantErr bool
	}{
		{name: "png signature", raw: pngMagic, want: "png"},
		{name: "png signature with payload", raw: append(append([]byte{}, pngMagic...), 0x00, 0x01), want: "png"},
		{name: "jpeg signature", raw: []byte{0xFF, 0xD8, 0xFF}, want: "jpg"},
		{name: "jpeg signature with payload", raw: []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, want: "jpg"},
		{name: "two byte jpeg prefix", raw: []byte{0xFF, 0xD8}, wantErr: true},
		{name: "seven byte png prefix", raw: pngMagic[:7], wantErr: true},
		{name: "empty", raw: nil, wantErr: true},
		{name: "text", raw: []byte("hello world"), wantErr: true},
		{name: "gif", raw: []byte("GIF89a"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detectImageFormat(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("detectImageFormat() returned nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("detectImageFormat() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("detectImageFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToPNG(t *testing.T) {
	pngData := gnPNGData(t, 4, 3)
	jpegData := gnJPEGData(t, 5, 4, 90)
	encode := base64.StdEncoding.EncodeToString

	tests := []struct {
		name       string
		input      string
		wantErr    bool
		wantSame   bool
		wantWidth  int
		wantHeight int
	}{
		{name: "raw base64 PNG", input: encode(pngData), wantSame: true},
		{name: "data URL PNG", input: "data:image/png;base64," + encode(pngData), wantSame: true},
		{name: "raw base64 JPEG converts to PNG", input: encode(jpegData), wantWidth: 5, wantHeight: 4},
		{name: "data URL JPEG converts to PNG", input: "data:image/jpeg;base64," + encode(jpegData), wantWidth: 5, wantHeight: 4},
		{name: "invalid base64", input: "$$$", wantErr: true},
		{name: "unsupported bytes", input: encode([]byte("GIF89a")), wantErr: true},
		{name: "empty input", input: "", wantErr: true},
		// Current behavior: a bare PNG signature is passed through without a
		// full decode, so callers must not rely on toPNG validating the body.
		{name: "PNG magic only passes through", input: encode(gnPNGMagic()), wantSame: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := toPNG(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("toPNG() returned nil error, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("toPNG() error = %v", err)
			}
			if !bytes.HasPrefix(out, gnPNGMagic()) {
				t.Fatalf("toPNG() output has no PNG signature: %x", out[:min(8, len(out))])
			}
			if tt.wantSame {
				raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(tt.input, "data:image/png;base64,"))
				if err != nil {
					t.Fatalf("failed to decode test input: %v", err)
				}
				if !bytes.Equal(out, raw) {
					t.Fatal("toPNG() changed PNG passthrough bytes")
				}
			}
			if tt.wantWidth > 0 {
				width, height := gnImageConfig(t, out)
				if width != tt.wantWidth || height != tt.wantHeight {
					t.Fatalf("toPNG() image = %dx%d, want %dx%d", width, height, tt.wantWidth, tt.wantHeight)
				}
			}
		})
	}
}

func TestSanitizeExportName(t *testing.T) {
	tests := []struct {
		name      string
		suggested string
		settings  Settings
		want      string // used when datedPrefix is empty
		dated     string // when set, expect dated+"-"+today
	}{
		{
			name:      "suggested name without extension",
			suggested: "my capture",
			settings:  DefaultSettings(),
			want:      "my capture",
		},
		{
			name:      "suggested name with png extension",
			suggested: "shot.png",
			settings:  DefaultSettings(),
			want:      "shot",
		},
		{
			name:      "suggested name with jpeg extension",
			suggested: "my.shot.jpeg",
			settings:  DefaultSettings(),
			want:      "my.shot",
		},
		{
			name:      "suggested name is trimmed",
			suggested: "  shot.jpg  ",
			settings:  DefaultSettings(),
			want:      "shot",
		},
		// Suggested names only get their extension stripped; they are a save
		// dialog default, so forbidden characters are not rewritten there.
		{
			name:      "suggested name keeps forbidden characters",
			suggested: "weird/name",
			settings:  DefaultSettings(),
			want:      "weird/name",
		},
		{
			name:      "extension-only suggested name falls back to prefix",
			suggested: ".png",
			settings:  DefaultSettings(),
			dated:     "Tupen",
		},
		{
			name:     "empty suggested name uses default prefix",
			settings: DefaultSettings(),
			dated:    "Tupen",
		},
		{
			name:      "blank suggested name uses default prefix",
			suggested: "   ",
			settings:  DefaultSettings(),
			dated:     "Tupen",
		},
		{
			name:     "empty prefix falls back to Tupen",
			settings: gnSettings(func(s *Settings) { s.FilenamePrefix = "" }),
			dated:    "Tupen",
		},
		{
			name:     "whitespace prefix falls back to Tupen",
			settings: gnSettings(func(s *Settings) { s.FilenamePrefix = "   " }),
			dated:    "Tupen",
		},
		{
			name:     "forbidden prefix characters become dashes",
			settings: gnSettings(func(s *Settings) { s.FilenamePrefix = `a/b\c:d*e?f"g<h>i|j` }),
			dated:    "a-b-c-d-e-f-g-h-i-j",
		},
		{
			name:     "allowed prefix characters are kept",
			settings: gnSettings(func(s *Settings) { s.FilenamePrefix = "My Shots_2026" }),
			dated:    "My Shots_2026",
		},
		{
			name:     "unicode prefix is kept",
			settings: gnSettings(func(s *Settings) { s.FilenamePrefix = "截图" }),
			dated:    "截图",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().Format("2006-01-02")
			got := sanitizeExportName(tt.suggested, tt.settings)
			after := time.Now().Format("2006-01-02")
			if tt.dated != "" {
				valid := false
				for _, date := range []string{before, after} {
					if got == tt.dated+"-"+date {
						valid = true
					}
				}
				if !valid {
					t.Fatalf("sanitizeExportName() = %q, want %q date suffix", got, tt.dated)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("sanitizeExportName(%q) = %q, want %q", tt.suggested, got, tt.want)
			}
		})
	}
}

func TestReplaceExtension(t *testing.T) {
	tests := []struct {
		path string
		ext  string
		want string
	}{
		{path: "shot.png", ext: "jpg", want: "shot.jpg"},
		{path: "shot", ext: "png", want: "shot.png"},
		{path: "a.b.c", ext: "jpg", want: "a.b.jpg"},
		{path: "dir/shot.PNG", ext: "png", want: "dir/shot.png"},
		{path: "shot.", ext: "png", want: "shot.png"},
		{path: ".hidden", ext: "png", want: ".hidden.png"},
		// Degenerate but documented: an empty path becomes just the extension.
		{path: "", ext: "png", want: ".png"},
	}
	for _, tt := range tests {
		t.Run(tt.path+"->"+tt.ext, func(t *testing.T) {
			if got := replaceExtension(tt.path, tt.ext); got != tt.want {
				t.Fatalf("replaceExtension(%q, %q) = %q, want %q", tt.path, tt.ext, got, tt.want)
			}
		})
	}
}
