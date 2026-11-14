# Pinstill · 钉图

**Screenshot → annotate in place → copy / pin / save → back to history**, all from one hotkey.
Pure Go + system WebView: no Electron, no cgo. Screenshots stay on your Mac.

[中文文档](README.md) · [Features](#features) · [Screenshots](#screenshots) · [Build from source](#build-from-source) · [Architecture](#architecture) · [Development](#development)

[![CI](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml/badge.svg)](https://github.com/echoVic/Pinstill/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-yellow.svg)](LICENSE)

![In-place annotation overlay](outputs/pinstill-overlay.jpg)

## What it is

Pinstill (pin + still) is a desktop screenshot and annotation tool for macOS (Windows build is
cross-compiled but not yet verified on real hardware). It keeps the whole capture workflow on the
desktop: select a region, annotate it in place, copy it, pin it on top of other windows, and browse
the capture history later.

- The screenshot appears **inside the selection** on your desktop, with the annotation toolbar
  floating next to it — no separate editor window.
- Pins are always-on-top borderless windows that survive app restarts.
- Everything is local: images are read through an internal `shot://` protocol, no network, no uploads.

## Features

- **Region capture + in-place toolbar**: full-screen selection (dim mask, crosshair, size badge),
  flicker-free capture through the transparent selection, then annotate with rectangle / ellipse /
  line / arrow / pen / text / tag / mosaic / highlight / watermark, plus undo, colours and width.
- **Text styles**: bold / italic, size 10–32pt, font family (system / PingFang / Helvetica /
  Georgia / Menlo), remembered across captures and previewed live in the inline editor.
- **Tiled watermark**: toggle a sparse, diagonal (-30°) repeated watermark over the whole image with
  configurable text, opacity, colour and size.
- **Palette panel**: 40 presets, SV square, hue slider, hex input, recent colours.
- **Pins**: drag to place, scroll to scale, ⌘/Ctrl+scroll for opacity, right-click menu, double-click
  to copy, Esc to close; positions/sizes/opacity restored after a restart.
- **Scrolling capture**: automatic scrolling and stitching by default, auto-finish when the page
  stops moving, tall-image result preview (copy / save / pin), manual fallback; the selection frame
  marks the region between frames and briefly hides during capture.
- **History**: capture cards with thumbnails, large preview, copy / save / pin / delete, retention by
  days and total size.
- **System integration**: menu-bar tray, global hotkeys (⌘⇧1 capture, ⌘⇧2 pin clipboard),
  7 settings categories, permission centre for screen recording / accessibility / launch at login.
- Double-click the canvas or press Enter to copy; Esc cancels; drag the selection corners to adjust
  the region and recapture.

OCR, translation and QR decoding are clearly marked as *not implemented yet*. OCR does not register or reserve a global hotkey.

## Screenshots

All screenshots are generated from synthetic demo images (`PINSTILL_SELFTEST=showcase`); no user
desktop content is included.

| | |
|---|---|
| ![Watermark](outputs/pinstill-watermark.jpg) | ![Pin](outputs/pinstill-pin.jpg) |
| ![Scrolling capture](outputs/pinstill-scroll.jpg) | ![Scroll result](outputs/pinstill-scroll-result.jpg) |
| ![History](outputs/pinstill-history.jpg) | ![Settings](outputs/pinstill-settings.jpg) |

## Build from source

Requirements: macOS 13+, Xcode Command Line Tools, Go (see [go.mod](go.mod)), Node.js 22+ and npm.

```bash
git clone https://github.com/echoVic/Pinstill.git
cd Pinstill
npm install
npm run build:web
./node_modules/.bin/mygo build        # or: npm run build
open build/darwin-arm64/Pinstill.app
```

Development mode with Vite HMR: `npm run dev`.

Local builds use ad-hoc signing by default and need no developer certificate. For a release,
set `MACOS_SIGNING_IDENTITY`; to notarize it, also set `MACOS_NOTARY_PROFILE` to a saved keychain profile.

On first launch, grant **Screen Recording** (required for captures); automatic scrolling capture
also needs **Accessibility**.

## Architecture

- **Shell**: [MyGo](https://mygo.egoist.dev/) (Go + system WebView, no cgo) — windows, tray, global
  shortcuts, clipboard, custom protocol, Go ↔ JS bindings/events.
- **Frontend**: Vite + TypeScript, one HTML entry per window: `index` (history/settings), `overlay`,
  `pin`, `scroll`, `result`, `frame`.
- **Go packages**: `internal/app` (orchestration), `internal/capture` (platform capture backends +
  permission preflight), `internal/store` (history), `internal/pin` (pins), `internal/stitch`
  (scroll stitching).
- Captures are served to the WebView through a custom protocol instead of base64.

## Development

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go vet ./...
npm test
npm run typecheck
npm run build:web

# end-to-end acceptance: full build + 24 app-level selftests
bash work/e2e.sh
bash work/e2e.sh --skip-build
```

The e2e suite covers real quit, real capture + history + clipboard, overlay rendering, selection
mapping, in-place annotation flows, pins (including restart restore), scrolling capture, history
actions, hotkey registration, settings, permissions, palette, text styles, watermark, and more.
Quit Pinstill first; the script stops if an instance is running. Each case gets a fresh temporary
data directory, with only the pin persistence setup/check pair sharing one. Logs and data are kept
in each isolated data directory's `artifacts/` folder. Direct `PINSTILL_SELFTEST` launches also require
an existing absolute `PINSTILL_DATA_DIR` containing a regular `.pinstill-selftest` file with `pinstill-selftest` followed by a newline. The script creates it automatically. Every case has a 120-second timeout, configurable with `PINSTILL_SELFTEST_TIMEOUT_SECONDS` (1–3600). Test windows use temporary WebView storage and do not change the installed
app's preferences or launch-at-login setting. Native tests still operate the desktop and clipboard.
`npm test` runs synthetic frontend and tooling checks without launching the app.

## Limitations

- Windows backend converts MyGo DIPs to monitor-local physical pixels and composes the cursor. Mixed DPI, negative origins and cursor hotspots have synthetic tests; real hardware remains unverified.
- OCR / translation / QR are not implemented.
- Scrolling capture depends on Accessibility. Static sticky headers have regression coverage; animated headers, fixed footers and indistinguishable repeated content remain heuristic. Limits are 120 frames, 30000px height or 32 Mi pixels, whichever comes first. Capture controls request OS exclusion and briefly hide during each capture; native compositor behavior remains unverified.
- The overlay covers the visible work area (menu bar and Dock excluded on macOS).
- Launch-at-login reports `Operation not permitted` when the app is not in `/Applications`.
- Mixed-DPI multi-monitor setups are not fully tested.
- Startup restores the newest 60 active pins and explicitly archives older ones without deleting images. Restore archived pins from History after closing enough active pins. Archived pins never reopen automatically; ordinary new pins have no runtime count cap.
- Images are limited to 64 MiB encoded data and 32 Mi pixels. A corresponding RGBA canvas alone occupies 128 MiB; decoder and WebView overhead is additional. Extreme aspect ratios may reduce one pin dimension to 1 DIP.
- Session storage retains only the active selection and result preview, with a staged replacement during writes. Failed cleanup blocks further additions until it can be retried; startup clears the cache.

## Contributing

Issues and PRs are welcome. Before submitting, please keep `gofmt`, `go vet`, `go test ./...` and
ideally `bash work/e2e.sh --skip-build` green, and avoid committing `build/`, `dist/`,
`node_modules/` or real screenshots of your desktop. Commit messages use conventional prefixes
(`feat:`, `fix:`, `docs:`, ...).

## License

[MIT](LICENSE) © 2026 echovic

## Acknowledgements

MyGo, Vite, Lucide and all upstream open-source dependencies.
