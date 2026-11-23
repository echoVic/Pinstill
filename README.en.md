# Pinstill · 钉图

[简体中文](README.md) | English

**Screenshot → annotate in place → copy / pin / hang / save**, all from one hotkey.
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
3. Drag any edge or corner to adjust the selection, then release to update the capture. Choose Scrolling Capture for a tall page.
4. Choose Pin for an always-on-top window or Hang to put the image on the desktop clothesline. After copying or saving, the screenshot card at the bottom right offers more actions.
5. From the menu bar, open Pin Manager to organize pins, toggle the desktop clothesline, or open History & Settings for captures and preferences.

Hotkeys and what happens after a capture (annotate / copy / pin) are configurable in Settings.

While selecting, use the arrow keys to fine-tune the endpoint and hold Shift for steps of 10 points. Press Z for the pixel loupe, C to copy the displayed color, or M to measure from the pointer. After capturing an area on one display, use Option + arrows to move the region or add Shift to adjust its size.

Press V to select existing annotations. Drag to move a shape, double-click text or a tag to edit it, and use the palette to recolor the selection. Delete removes it, ⌘Z undoes, and ⌘⇧Z redoes.

## Features

- **Capture and annotation** offers region and full-screen capture with a dim mask, crosshair and size badge. On macOS you can select across displays, with a fitted preview for annotation. Draw rectangles, ellipses, lines, arrows, freehand strokes, text, tags, mosaic, highlights and tiled watermarks using a 40-colour palette, hex input and text styles.
- **Screenshot card** appears after you copy or save a region, full-screen, or scrolling capture. Copy, save, pin, or hang the image from the card. It stays open on hover and closes about six seconds after you move away.
- **Pins** are always-on-top windows you can drag, resize, fade, rotate, and flip. Use Pin Manager to group pins, show or hide a group, and restore mouse clicks to click-through pins. Pin settings are restored with the images. Up to 60 pins stay active, with older ones archived.
- **Desktop clothesline** displays a row of pictures centred below the menu bar on the primary Mac display and floats above ordinary windows while visible. Hang a capture from the toolbar or a preview, or add an image from a file or the clipboard. Drag to reorder. Select a picture to copy, save, pin, or remove it. Pictures, order, and visibility are restored when Pinstill restarts.
- **History** displays local thumbnails. The large preview supports copy, save, pin, hang, and delete. Retention by age and size is configurable.
- **Scrolling capture** starts in manual mode. Scroll and choose Finish now when ready, or enable automatic scrolling from the controls with Accessibility permission. If frames cannot be matched, the app keeps the stitched image and prompts you to scroll back slightly. Capture finishes at 120 frames, 30,000 px or 32 Mi pixels.
- **Tray and hotkeys**: a menu-bar icon; **⌘⇧1** captures a region and **⌘⇧2** pins the clipboard image, both re-recordable in Settings.
- **Settings and permissions**: seven categories take effect immediately; the permission centre shows the real state of screen recording, Accessibility and launch at login.

OCR, translation and QR decoding are still placeholders marked *not implemented yet*; they say so when opened instead of pretending to work.

## Data and permissions

- Captures live in `~/Library/Application Support/Pinstill/captures/`, pins in `.../pins/`, clothesline images in `.../clothesline/`, and the session cache in `~/Library/Caches/Pinstill/session/`.
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
- On macOS, a cross-display capture uses the highest pixel density in the selection and keeps any gaps between displays transparent. Scrolling capture needs a region within one display.
- Launch at login fails when the app isn't in `/Applications`; the settings page reports that as it is.

## Contributing

Issues and pull requests are welcome. Before submitting, make sure `gofmt -l .` prints nothing, and that `go vet ./...` and `go test ./...` pass; run `bash work/e2e.sh --skip-build` after frontend or interaction changes. Prefix commit messages with `feat: / fix: / docs: / perf: / test:` and explain why, not just what.

## Credits and license

Desktop shell by [MyGo](https://mygo.egoist.dev/) (Go + system WebView); frontend built with Vite; icons from Lucide (ISC). [MIT](LICENSE) © 2026 echovic.
