# Pinstill · 钉图

[简体中文](README.md) | English

**Screenshot → annotate in place → copy / pin / save → back to history**, all from one hotkey.
Pure Go + system WebView: no Electron, no cgo. Screenshots stay on your Mac.

[![CI](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml/badge.svg)](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)
![macOS](https://img.shields.io/badge/platform-macOS%2013%2B-black)
![Windows](https://img.shields.io/badge/platform-Windows%20(cross--compiled)-lightgrey)

![In-place annotation overlay](outputs/pinstill-overlay.jpg)

## Download

Download the macOS build from [Releases](https://github.com/echoVic/Pinstill/releases); it needs macOS 13 or later. The Windows capture backend (GDI) cross-compiles, but it hasn't been verified on real hardware, so there is no Windows installer yet.

Builds with automatic updates check in the background, or you can choose “检查更新…” (Check for Updates) from the tray or Pinstill menu. By default they ask before downloading and installing. If you are on `v0.1.2` or older, upgrade manually once. [Release instructions for maintainers](docs/releasing.md)

## Usage

1. Pinstill lives in the menu bar; press **⌘⇧1** (or choose Capture Region from the tray) to start a selection.
2. Drag a region and release to capture; the annotation toolbar appears next to it. Press Enter or double-click the canvas to copy, Esc to cancel.
3. Drag the selection corners to adjust the region and capture again; choose Scrolling Capture for a tall page.
4. Pin puts the shot on top of other windows, Save opens the system save dialog, Copy puts it on the clipboard.
5. Tray → History & Settings for captures, pins and all preferences.

Hotkeys and what happens after a capture (annotate / copy / pin) are configurable in Settings.

## Features

- **Capture and annotation**: region and full-screen capture with a dim mask, crosshair and size badge; the shot appears inside the selection with the full toolbar beside it. Rectangle, ellipse, line, arrow, pen, text, tag, mosaic, highlight and tiled watermark, with undo, a 40-colour palette, hex input and text styles.
- **Pins**: always-on-top borderless windows you can drag, scale and fade, with a right-click menu for copy, save, opacity and original size; each keeps its own place and they come back after a restart (up to 60 active pins, older ones archived).
- **History**: captures are saved locally with real thumbnails; a large preview supports copy, save, pin and delete, and retention by days and size applies.
- **Scrolling capture**: automatic scrolling and stitching that finishes at 120 frames, 30,000px or 32 Mi pixels, and can be paused, switched to manual or finished early. It needs Accessibility permission and falls back to manual scrolling without it.
- **Tray and hotkeys**: a menu-bar icon; **⌘⇧1** captures a region and **⌘⇧2** pins the clipboard image, both re-recordable in Settings.
- **Settings and permissions**: seven categories take effect immediately; the permission centre shows the real state of screen recording, Accessibility and launch at login.

OCR, translation and QR decoding are still placeholders marked *not implemented yet*; they say so when opened instead of pretending to work.

## Data and permissions

- Captures live in `~/Library/Application Support/Pinstill/captures/`, pins in `.../pins/`, and the session cache in `~/Library/Caches/Pinstill/session/`.
- Pages read those images through `shot://localhost/...`; nothing goes over the network and nothing is uploaded.
- Screen Recording permission is needed for captures; automatic scrolling also needs Accessibility, and falls back to manual without it.
- For development or self-tests, set `PINSTILL_DATA_DIR=/tmp/pinstill-data` so the app can't read your real history.

## Development

You need Go 1.27+ (see `go.mod`), Node 22+ and npm.

```bash
npm install
npm run build:web          # build the frontend into dist/
npm run build              # or ./node_modules/.bin/mygo build
npm run dev                # Vite HMR with the MyGo window
```

Checks (the same ones CI runs):

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go vet ./...
npm test
npm run typecheck
bash work/e2e.sh --skip-build   # 24 app-level self-tests; drop --skip-build to build first
```

`go test -tags integration ./internal/capture` exercises the real capture backend and needs Screen Recording permission.

## Known limitations

- Windows has only cross-compilation and synthetic tests; mixed DPI, negative coordinates and multi-monitor setups still need real-hardware validation.
- OCR, translation and QR decoding aren't wired up; scrolling capture can miss self-drawn scrolling, dynamic headers and fully repeating content.
- The overlay covers the visible workspace, so the menu bar and Dock can't be selected directly; multi-monitor and mixed-scaling setups are not fully tested.
- Launch at login fails when the app isn't in `/Applications`; the settings page reports that as it is.

## Contributing

Issues and pull requests are welcome. Before submitting, make sure `gofmt -l .` prints nothing, and that `go vet ./...` and `go test ./...` pass; run `bash work/e2e.sh --skip-build` after frontend or interaction changes. Prefix commit messages with `feat: / fix: / docs: / perf: / test:` and explain why, not just what.

## Credits and license

Desktop shell by [MyGo](https://mygo.egoist.dev/) (Go + system WebView); frontend built with Vite; icons from Lucide (ISC). [MIT](LICENSE) © 2026 echovic.
