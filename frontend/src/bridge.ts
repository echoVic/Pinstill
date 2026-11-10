// The single entry point for talking to the native (Go) side of Pinstill.
//
// Every bound method from work/plan.md §3.3/§3.6 is wrapped here on top of the
// untyped `mygo-runtime` API, with the exact names, arguments and return values
// of the contract. When the page runs in a plain browser (`hasNative === false`,
// e.g. `npm run dev:web` static preview) the wrappers degrade gracefully to the
// design prototype's simulation: nothing throws at import time, and the preview
// stays clickable.

import { call, isMyGo, on } from "mygo-runtime";
import { demoShotDataURL, demoThumbDataURL } from "./demo";
import { base64ToBytes, downloadBlob } from "./util";

/** `true` when the page runs inside a MyGo window. */
export const hasNative = isMyGo();

/** A rectangle in CSS pixels, local to the overlay page for selections. */
export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** Display geometry the overlay page runs on. */
export interface OverlayInfo {
  displayX: number;
  displayY: number;
  width: number;
  height: number;
  scale: number;
}

/** One stored capture, as listed by the `History` service. */
export interface HistoryEntry {
  id: string;
  /** RFC 3339 timestamp. */
  createdAt: string;
  width: number;
  height: number;
  size: number;
  /** Absolute URL prepared by Go (`shot://localhost/…` etc.). */
  url: string;
  thumbUrl: string;
}

/** What the in-place selection action bar asks Go to do with the selection. */
export type SelectionAction = "annotate" | "copy" | "pin" | "save";

/** Size and opacity of a pin window, persisted by Go. */
export interface PinState {
  width: number;
  height: number;
  opacity: number;
}

/** Payload of `scroll:init`: sent to the scroll control bar window. */
export interface ScrollInit {
  /** The selection, in overlay-local CSS pixels (for reference text only). */
  rect: Rect;
  /** Whether the session started in automatic mode. */
  auto: boolean;
  /** Whether macOS Accessibility is granted (needed for automatic scrolling). */
  accessibilityGranted: boolean;
}

/** One `scroll:progress` event of a scrolling-capture session. */
export interface ScrollProgress {
  frames: number;
  /** Total stitched height in image pixels. */
  height: number;
  status: "running" | "done" | "stalled" | "max" | "stopped";
  /** Current automatic-scrolling state, when Go reports it. */
  auto?: boolean;
}

/** A selection captured by `Capture.PrepareSelection`, ready for in-place annotation. */
export interface OverlayCapture {
  id: string;
  /** Go-provided absolute URL of the captured PNG. */
  url: string;
  /** Image pixels. */
  width: number;
  height: number;
  /** The selection in overlay-local CSS pixels. */
  rect: Rect;
}

/** What `Capture.FinishAnnotated` does with the composited PNG. */
export type FinishAnnotatedAction = "annotate" | "copy" | "pin" | "save" | "cancel";

/** Payload of the `capture:completed` event. */
export interface CaptureCompleted {
  id: string;
  width: number;
  height: number;
  url: string;
  /** Present when the capture was written to history. */
  thumbUrl?: string;
  source: "region" | "screen" | "clipboard" | "scroll";
}

/** Settings object pushed to `Capture.ApplySettings` (plan §3.4). */
export interface Settings {
  /** MyGo accelerator string, e.g. "CmdOrCtrl+Shift+1". */
  captureKey: string;
  pinKey: string;
  ocrKey: string;
  /** "inplace" is the default: the selection shows the full annotation toolbar. */
  afterCapture: "inplace" | "clipboard" | "pin" | "editor";
  format: "PNG" | "JPEG";
  /** 50..100, used for JPEG exports. */
  quality: number;
  filenamePrefix: string;
  /** e.g. "~/Pictures/Pinstill"; Go expands `~`. */
  folder: string;
  includeCursor: boolean;
  sound: boolean;
  launchAtLogin: boolean;
  keepHistory: boolean;
  /** 0 = keep forever. */
  retentionDays: number;
  /** 0 = unlimited. */
  historyLimitMB: number;
  tooltip: boolean;
  /** 30..100, pinned window opacity. */
  pinOpacity?: number;
  pinBorder?: boolean;
}

// ---------------------------------------------------------------------------
// Native calls (throws only inside MyGo windows; browser preview is handled
// by the fallbacks below).
// ---------------------------------------------------------------------------

export function beginRegion(): Promise<void> {
  if (!hasNative) return browserBeginRegion();
  return call("Capture.BeginRegion");
}

export function cancelRegion(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.CancelRegion");
}

export function finishRegion(rect: Rect): Promise<{ id: string }> {
  if (!hasNative) return Promise.resolve({ id: "browser-preview" });
  return call<{ id: string }>("Capture.FinishRegion", rect);
}

export function captureFullScreen(): Promise<{ id: string }> {
  if (!hasNative) return Promise.resolve({ id: "browser-preview" });
  return call<{ id: string }>("Capture.CaptureFullScreen");
}

export function copyImage(base64: string): Promise<void> {
  if (!hasNative) return browserCopyImage(base64);
  return call("Capture.CopyImage", base64);
}

export function exportImage(base64: string, suggestedName: string): Promise<string> {
  if (!hasNative) return browserDownload(base64, suggestedName);
  return call<string>("Capture.ExportImage", base64, suggestedName);
}

export function saveImageToFolder(base64: string): Promise<string> {
  if (!hasNative) return browserDownload(base64, "Pinstill.png");
  return call<string>("Capture.SaveImageToFolder", base64);
}

export function pinImage(base64: string): Promise<string> {
  if (!hasNative) return Promise.resolve("browser-preview-pin");
  return call<string>("Capture.PinImage", base64);
}

export function pinClipboard(): Promise<string> {
  if (!hasNative) return Promise.resolve("browser-preview-pin");
  return call<string>("Capture.PinClipboard");
}

export function closePin(id: string): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.ClosePin", id);
}

export function completeSelection(rect: Rect, action: SelectionAction): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.CompleteSelection", rect, action);
}

export function resizePin(id: string, width: number, height: number): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.ResizePin", id, width, height);
}

export function setPinOpacity(id: string, opacity: number): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.SetPinOpacity", id, opacity);
}

export function copyPin(id: string): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.CopyPin", id);
}

export function savePin(id: string): Promise<string> {
  if (!hasNative) return Promise.resolve("");
  return call<string>("Capture.SavePin", id);
}

export function updatePinState(id: string, state: PinState): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.UpdatePinState", id, state);
}

export function settings(): Promise<Settings> {
  if (!hasNative) return Promise.resolve({ ...BROWSER_DEFAULT_SETTINGS });
  return call<Settings>("Capture.Settings");
}

export function prepareSelection(rect: Rect): Promise<OverlayCapture> {
  if (!hasNative) return Promise.resolve(browserCapture(rect));
  return call<OverlayCapture>("Capture.PrepareSelection", rect);
}

export function finishAnnotated(
  action: FinishAnnotatedAction,
  dataBase64: string,
): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.FinishAnnotated", action, dataBase64);
}

// ------------------------------------------------- scrolling capture (R4.2)

export function startScroll(rect: Rect, autoScroll: boolean): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.StartScroll", rect, autoScroll);
}

export function finishScroll(action: FinishAnnotatedAction): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.FinishScroll", action);
}

export function setScrollAuto(enabled: boolean): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.SetScrollAuto", enabled);
}

export function scrollAccessibilityGranted(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(false);
  return call<boolean>("Capture.ScrollAccessibilityGranted");
}

export function openScrollAccessibilitySettings(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.OpenScrollAccessibilitySettings");
}

export function applySettings(settings: Settings): Promise<Record<string, string>> {
  if (!hasNative) return Promise.resolve({});
  return call<Record<string, string>>("Capture.ApplySettings", settings);
}

export function permissionGranted(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(true);
  return call<boolean>("Capture.PermissionGranted");
}

export function requestScreenPermission(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(true);
  return call<boolean>("Capture.RequestScreenPermission");
}

export function openPermissionSettings(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.OpenPermissionSettings");
}

// ------------------------------------------------- permission centre (R7.1)

/** System state of the OS login item, independent of the saved preference. */
export function launchAtLogin(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(false);
  return call<boolean>("Capture.LaunchAtLogin");
}

/** Opens the OS login-items settings pane (macOS; unsupported elsewhere). */
export function openLoginItemsSettings(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.OpenLoginItemsSettings");
}

export function overlayInfo(): Promise<OverlayInfo> {
  if (!hasNative) {
    return Promise.resolve({
      displayX: 0,
      displayY: 0,
      width: window.innerWidth,
      height: window.innerHeight,
      scale: window.devicePixelRatio || 1,
    });
  }
  return call<OverlayInfo>("Capture.OverlayInfo");
}

export function showMainWindow(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return call("Capture.ShowMainWindow");
}

export function historyList(): Promise<HistoryEntry[]> {
  if (!hasNative) return Promise.resolve(mockHistory().map((entry) => ({ ...entry })));
  return call<HistoryEntry[]>("History.List");
}

export function historyGet(
  id: string,
): Promise<{ id: string; width: number; height: number; url: string }> {
  if (!hasNative) {
    const entry = mockHistory().find((item) => item.id === id);
    if (!entry) return Promise.reject(new Error(`找不到截图 ${id}`));
    return Promise.resolve({ id: entry.id, width: entry.width, height: entry.height, url: entry.url });
  }
  return call<{ id: string; width: number; height: number; url: string }>("History.Get", id);
}

export function historyRemove(id: string): Promise<void> {
  if (!hasNative) {
    mockEntries = mockEntries.filter((entry) => entry.id !== id);
    return Promise.resolve();
  }
  return call("History.Remove", id);
}

export function historyClear(): Promise<void> {
  if (!hasNative) {
    mockEntries = [];
    return Promise.resolve();
  }
  return call("History.Clear");
}

// ---------------------------------------------------------------------------
// Events (colon-separated names from plan §3.5, raw untyped API).
// ---------------------------------------------------------------------------

function subscribe<T>(name: string, listener: (payload: T) => void): () => void {
  if (!hasNative) return () => {};
  try {
    return on<T>(name, listener);
  } catch {
    // A page outside a MyGo window has no runtime; keep the subscription a no-op.
    return () => {};
  }
}

export function onCaptureCompleted(cb: (payload: CaptureCompleted) => void): () => void {
  return subscribe("capture:completed", cb);
}

export function onCaptureCancelled(cb: (payload: { reason: string }) => void): () => void {
  return subscribe("capture:cancelled", cb);
}

export function onCaptureFailed(
  cb: (payload: { message: string; permission: boolean }) => void,
): () => void {
  return subscribe("capture:failed", cb);
}

export function onOverlayShown(cb: (payload: OverlayInfo) => void): () => void {
  return subscribe("overlay:shown", cb);
}

export function onOverlayCapture(cb: (payload: OverlayCapture) => void): () => void {
  return subscribe("overlay:capture", cb);
}

export function onScrollInit(cb: (payload: ScrollInit) => void): () => void {
  return subscribe("scroll:init", cb);
}

export function onScrollProgress(cb: (payload: ScrollProgress) => void): () => void {
  return subscribe("scroll:progress", cb);
}

export function onHistoryChanged(cb: () => void): () => void {
  return subscribe("history:changed", cb);
}

export function onToast(cb: (payload: { text: string; kind: string }) => void): () => void {
  return subscribe("toast", cb);
}

// ---------------------------------------------------------------------------
// Browser-preview fallbacks (the design prototype, made safe).
// ---------------------------------------------------------------------------

function browserBeginRegion(): Promise<void> {
  // No overlay windows exist in a browser tab; the main page shows the design's
  // demo screenshot instead (see main.ts).
  return Promise.resolve();
}

function browserCopyImage(base64: string): Promise<void> {
  const bytes = base64ToBytes(base64);
  const blob = new Blob([bytes], { type: "image/png" });
  if (navigator.clipboard?.write && typeof ClipboardItem !== "undefined") {
    return navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]).catch(() => {
      throw new Error("clipboard-unavailable");
    });
  }
  return Promise.reject(new Error("clipboard-unavailable"));
}

async function browserDownload(base64: string, suggestedName: string): Promise<string> {
  const filename = /\.[a-z0-9]+$/i.test(suggestedName) ? suggestedName : `${suggestedName}.png`;
  downloadBlob(new Blob([base64ToBytes(base64)], { type: "image/png" }), filename);
  return filename;
}

function browserCapture(rect: Rect): OverlayCapture {
  // Two device pixels per CSS pixel keeps the demo capture looking like a
  // Retina screenshot; the canvas stays untainted (data URL from our own canvas).
  return {
    id: "browser-capture",
    url: demoShotDataURL(),
    width: Math.max(1, Math.round(rect.width * 2)),
    height: Math.max(1, Math.round(rect.height * 2)),
    rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
  };
}

const BROWSER_DEFAULT_SETTINGS: Settings = {
  captureKey: "CmdOrCtrl+Shift+1",
  pinKey: "CmdOrCtrl+Shift+2",
  ocrKey: "CmdOrCtrl+Shift+O",
  afterCapture: "inplace",
  format: "PNG",
  quality: 90,
  filenamePrefix: "Pinstill",
  folder: "~/Pictures/Pinstill",
  includeCursor: false,
  sound: true,
  launchAtLogin: false,
  keepHistory: true,
  retentionDays: 30,
  historyLimitMB: 2048,
  tooltip: true,
  pinOpacity: 100,
  pinBorder: true,
};

let mockEntries: HistoryEntry[] | null = null;

function mockHistory(): HistoryEntry[] {
  if (mockEntries) return mockEntries;
  const now = Date.now();
  const offsets = [2 * 60_000, 26 * 60 * 60_000, 3 * 24 * 60 * 60_000];
  mockEntries = offsets.map((offset, index) => ({
    id: `demo-${index + 1}`,
    createdAt: new Date(now - offset).toISOString(),
    width: 960,
    height: 480,
    size: 486_000 - index * 64_000,
    url: demoShotDataURL(),
    thumbUrl: demoThumbDataURL(index),
  }));
  return mockEntries;
}
