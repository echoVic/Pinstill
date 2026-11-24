// The single entry point for talking to the native (Go) side of Pinstill.
//
// Every bound method from work/plan.md §3.3/§3.6 is wrapped here on top of the
// generated services and events, with the exact arguments and return values
// of the contract. When the page runs in a plain browser (`hasNative === false`,
// e.g. `npm run dev:web` static preview) the wrappers degrade gracefully to the
// design prototype's simulation: nothing throws at import time, and the preview
// stays clickable.

import { isMyGo } from "mygo-runtime";
import { Capture, History, events } from "./mygo";
import type * as Native from "./mygo";
import { downloadBlob } from "./util";

/** `true` when the page runs inside a MyGo window. */
export const hasNative = isMyGo();

/** A rectangle in CSS pixels, local to the overlay page for selections. */
export type Rect = Native.Rect;

/** Display geometry the overlay page runs on. */
export type OverlayInfo = Native.OverlayInfo;
export type OverlayPreview = Native.OverlayPreviewPayload;

/** Visible desktop window projected into overlay-local CSS pixels. */
export type OverlayWindow = Native.OverlayWindow;

/** One stored capture, as listed by the `History` service. */
export type HistoryEntry = Native.HistoryEntry;

/** Size and opacity of a pin window, persisted by Go. */
export type PinState = Native.PinWindowState;
export type PinArchiveEntry = Native.PinArchiveEntry;

export function listArchivedPins(): Promise<PinArchiveEntry[]> {
  if (!hasNative) return Promise.resolve([]);
  return Capture.listArchivedPins();
}

export function restoreArchivedPin(id: string): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.restoreArchivedPin(id);
}

/** Payload of `scroll:init`: sent to the scroll control bar window. */
export type ScrollInit = Native.ScrollInitPayload;

/** One `scroll:progress` event of a scrolling-capture session. */
export interface ScrollProgress {
  sessionId: string;
  frames: number;
  /** Total stitched height in image pixels. */
  height: number;
  status: "running" | "paused" | "stalled" | "unaligned" | "max";
  /** Current automatic-scrolling state, when Go reports it. */
  auto?: boolean;
}

/** Payload of `scroll:result`: sent to the result preview window (R9). */
export type ScrollResult = Native.ScrollResultPayload;

/** What `Capture.FinishScroll` does with a running session. */
export type ScrollFinishAction = "finish" | "cancel";

/** A selection captured by `Capture.PrepareSelection`, ready for in-place annotation. */
export type OverlayCapture = Native.OverlayCapturePayload;

/** What `Capture.FinishAnnotated` does with the composited PNG. */
export type FinishAnnotatedAction = "copy" | "pin" | "hang" | "save" | "cancel";

export type CaptureActionResult = Native.CaptureActionResult;

/** Settings object pushed to `Capture.ApplySettings` (plan §3.4). */
export interface Settings {
  /** MyGo accelerator string, e.g. "CmdOrCtrl+Shift+1". */
  captureKey: string;
  pinKey: string;
  ocrKey: string;
  /** "inplace" is the default: the selection shows the full annotation toolbar. */
  afterCapture: "inplace" | "clipboard" | "pin";
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
  return Capture.beginRegion();
}

export function cancelRegion(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.cancelRegion();
}

export function overlayReady(session: number): Promise<void> {
  if (!hasNative || !Number.isFinite(session) || session <= 0) return Promise.resolve();
  return Capture.overlayReady(session);
}


export function captureFullScreen(): Promise<{ id: string }> {
  if (!hasNative) return Promise.resolve({ id: "browser-preview" });
  return Capture.captureFullScreen();
}

export function pinClipboard(): Promise<string> {
  if (!hasNative) return Promise.resolve("browser-preview-pin");
  return Capture.pinClipboard();
}

export function closePin(id: string): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.closePin(id);
}

export type ManagedPin = Native.ManagedPin;
export type PinPresentation = Native.PinPresentation;

export function listManagedPins(): Promise<ManagedPin[]> {
  return hasNative ? Capture.listManagedPins() : Promise.resolve([]);
}

export function managePin(id: string, action: string, value = ""): Promise<PinPresentation> {
  return Capture.managePin(id, action, value);
}

export function managePinGroup(group: string, action: string, value = ""): Promise<void> {
  return Capture.managePinGroup(group, action, value);
}

export function showPinManager(): Promise<void> {
  return hasNative ? Capture.showPinManager() : Promise.resolve();
}

export const onPinPresentation = (callback: (payload: PinPresentation) => void) =>
  subscribe(events.pinPresentation, callback);
export const onManagedPinsChanged = (callback: () => void) =>
  subscribe(events.pinManaged, callback);

export type LineView = Native.LineView;

export function listClothesline(): Promise<LineView> {
  return hasNative ? Capture.listClothesline() : Promise.resolve({ revision: 0, entries: [] });
}

export const onLineChanged = (callback: (view: LineView) => void) => subscribe(events.lineChanged, callback);

export function hideClothesline(): Promise<void> {
  return hasNative ? Capture.hideClothesline() : Promise.resolve();
}

export function importClotheslineImage(): Promise<boolean> {
  return hasNative ? Capture.importClotheslineImage() : Promise.resolve(false);
}

export function hangClipboard(): Promise<void> {
  return hasNative ? Capture.hangClipboard() : Promise.reject(new Error("桌面应用才能读取剪贴板图片"));
}

export function moveClotheslineImage(id: string, before: string): Promise<void> {
  return hasNative ? Capture.moveClotheslineImage(id, before) : Promise.resolve();
}

export function removeClotheslineImage(id: string): Promise<void> {
  return hasNative ? Capture.removeClotheslineImage(id) : Promise.resolve();
}

export function useClotheslineImage(id: string, action: "copy" | "pin" | "save"): Promise<boolean> {
  return hasNative ? Capture.useClotheslineImage(id, action) : Promise.resolve(false);
}

export function resizePin(id: string, width: number, height: number): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.resizePin(id, width, height);
}

export function setPinOpacity(id: string, opacity: number): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.setPinOpacity(id, opacity);
}

export function copyPin(id: string): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.copyPin(id);
}

export function savePin(id: string): Promise<string> {
  if (!hasNative) return Promise.resolve("");
  return Capture.savePin(id);
}

export function updatePinState(id: string, state: PinState): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.updatePinState(id, state);
}

export function settings(): Promise<Settings> {
  if (!hasNative) return Promise.resolve({ ...BROWSER_DEFAULT_SETTINGS });
  return Capture.settings().then((value) => value as Settings);
}

export function prepareSelection(rect: Rect, signal: AbortSignal): Promise<OverlayCapture> {
  if (!hasNative) return Promise.resolve(browserCapture(rect));
  return Capture.prepareSelection.with({ signal })(rect);
}

/** Re-capture after the page already hid its chrome (used when resizing the
 * selection): skips most of the compositor wait so the new image appears fast. */
export function prepareSelectionFast(rect: Rect, signal: AbortSignal): Promise<OverlayCapture> {
  if (!hasNative) return Promise.resolve(browserCapture(rect));
  return Capture.prepareSelectionFast.with({ signal })(rect);
}

export type PixelSample = Native.PixelSample;

export function sampleOverlayPixels(x: number, y: number): Promise<PixelSample> {
  if (!hasNative) return Promise.reject(new Error("桌面应用才能读取屏幕像素"));
  return Capture.sampleOverlayPixels(x, y);
}

export async function copyPixelColor(hex: string): Promise<void> {
  if (hasNative) return Capture.copyPixelColor(hex);
  await navigator.clipboard.writeText(hex);
}

export type RecognitionMode = "ocr" | "qr";
export type RecognitionCode = Native.RecognitionCode;

export function recognizeSelection(
  mode: RecognitionMode,
  language: string,
  pngBase64: string,
  signal: AbortSignal,
): Promise<Native.RecognitionResult> {
  return Capture.recognizeSelection.with({ signal })(mode, language, pngBase64);
}

export function copyRecognizedText(text: string, signal: AbortSignal): Promise<void> {
  return Capture.copyRecognizedText.with({ signal })(text);
}

export function openRecognizedLink(url: string, signal: AbortSignal): Promise<void> {
  return Capture.openRecognizedLink.with({ signal })(url);
}

export function finishAnnotated(
  action: FinishAnnotatedAction,
  dataBase64: string,
): Promise<CaptureActionResult> {
  if (!hasNative) return Promise.resolve({ completed: action !== "cancel", cancelled: action === "cancel" });
  return Capture.finishAnnotated(action, dataBase64);
}

// ------------------------------------------------- scrolling capture (R4.2)

export function startScroll(rect: Rect): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.startScroll(rect);
}

export function finishScroll(sessionId: string, action: ScrollFinishAction): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.finishScroll(sessionId, action);
}

export function setScrollAuto(sessionId: string, enabled: boolean): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.setScrollAuto(sessionId, enabled);
}

/** Pauses/resumes frame capture for the running scrolling session (R9). */
export function setScrollPaused(sessionId: string, paused: boolean): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.setScrollPaused(sessionId, paused);
}

// ------------------------------------------------- scroll result window (R9)

function requireScrollResultID(id: string): void {
  if (typeof id !== "string" || !id.trim()) throw new Error("缺少长图结果 ID");
}

export async function copyScrollResult(id: string): Promise<void> {
  requireScrollResultID(id);
  if (!hasNative) return;
  return Capture.copyScrollResult(id);
}

export async function saveScrollResult(id: string): Promise<string> {
  requireScrollResultID(id);
  if (!hasNative) return "";
  return Capture.saveScrollResult(id);
}

export async function pinScrollResult(id: string): Promise<string> {
  requireScrollResultID(id);
  if (!hasNative) return "browser-preview-pin";
  return Capture.pinScrollResult(id);
}

export async function hangScrollResult(id: string): Promise<void> {
  requireScrollResultID(id);
  if (hasNative) await Capture.hangScrollResult(id);
}

export async function closeScrollResult(id: string): Promise<void> {
  requireScrollResultID(id);
  if (!hasNative) return;
  return Capture.closeScrollResult(id);
}

export function scrollAccessibilityGranted(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(false);
  return Capture.scrollAccessibilityGranted();
}

export function openScrollAccessibilitySettings(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.openScrollAccessibilitySettings();
}

export function applySettings(settings: Settings): Promise<Record<string, string>> {
  if (!hasNative) return Promise.resolve({});
  return Capture.applySettings({ ...settings, pinOpacity: settings.pinOpacity ?? 100, pinBorder: settings.pinBorder ?? true });
}

export function permissionGranted(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(true);
  return Capture.permissionGranted();
}

export function requestScreenPermission(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(true);
  return Capture.requestScreenPermission();
}

export function openPermissionSettings(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.openPermissionSettings();
}

// ------------------------------------------------- permission centre (R7.1)

/** System state of the OS login item, independent of the saved preference. */
export function launchAtLogin(): Promise<boolean> {
  if (!hasNative) return Promise.resolve(false);
  return Capture.launchAtLogin();
}

/** Opens the OS login-items settings pane (macOS; unsupported elsewhere). */
export function openLoginItemsSettings(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.openLoginItemsSettings();
}

export function overlayInfo(): Promise<OverlayInfo> {
  if (!hasNative) {
    const width = window.innerWidth;
    const height = window.innerHeight;
    return Promise.resolve({
      displayX: 0,
      displayY: 0,
      width,
      height,
      scale: window.devicePixelRatio || 1,
      cursorX: 0,
      cursorY: 0,
      windows: [
        {
          x: Math.round(width * 0.14),
          y: Math.round(height * 0.14),
          width: Math.max(120, Math.round(width * 0.44)),
          height: Math.max(100, Math.round(height * 0.56)),
          owner: "Safari",
          title: "设计评审",
        },
        {
          x: Math.round(width * 0.42),
          y: Math.round(height * 0.24),
          width: Math.max(120, Math.round(width * 0.44)),
          height: Math.max(100, Math.round(height * 0.54)),
          owner: "Terminal",
          title: "Pinstill",
        },
      ],
    });
  }
  return Capture.overlayInfo();
}

export function showMainWindow(): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.showMainWindow();
}

export function historyList(): Promise<HistoryEntry[]> {
  if (!hasNative) return Promise.resolve(mockHistory().map((entry) => ({ ...entry })));
  return History.list();
}

export function historyGet(
  id: string,
): Promise<{ id: string; width: number; height: number; url: string }> {
  if (!hasNative) {
    const entry = mockHistory().find((item) => item.id === id);
    if (!entry) return Promise.reject(new Error(`找不到截图 ${id}`));
    return Promise.resolve({ id: entry.id, width: entry.width, height: entry.height, url: entry.url });
  }
  return History.get(id);
}

export function historyCopy(id: string): Promise<void> {
  if (!hasNative) return browserHistoryCopy(id);
  return History.copy(id);
}

export function historySave(id: string): Promise<string> {
  if (!hasNative) return browserHistorySave(id);
  return History.save(id);
}

export function historyPin(id: string): Promise<string> {
  if (!hasNative) return Promise.resolve("browser-preview-pin");
  return History.pin(id);
}

export function historyHang(id: string): Promise<void> {
  return hasNative ? History.hang(id) : Promise.resolve();
}

export function historyRemove(id: string): Promise<void> {
  if (!hasNative) {
    mockEntries = mockHistory().filter((entry) => entry.id !== id);
    return Promise.resolve();
  }
  return History.remove(id);
}

export function historyClear(): Promise<void> {
  if (!hasNative) {
    mockEntries = [];
    return Promise.resolve();
  }
  return History.clear();
}

// ---------------------------------------------------------------------------
// Events from the generated native contract.
// ---------------------------------------------------------------------------

function subscribe<T>(event: { on: (listener: (payload: T) => void) => () => void }, listener: (payload: T) => void): () => void {
  return hasNative ? event.on(listener) : () => {};
}

export const onCaptureCancelled = (cb: (payload: Native.CaptureCancelledPayload) => void) => subscribe(events.captureCancelled, cb);
export const onCaptureFailed = (cb: (payload: Native.CaptureFailedPayload) => void) => subscribe(events.captureFailed, cb);
export const onOverlayShown = (cb: (payload: OverlayInfo) => void) => subscribe(events.overlayShown, cb);
export const onOverlayHidden = (cb: (session: number) => void) => subscribe(events.overlayHidden, cb);
export const onOverlayPreview = (cb: (payload: OverlayPreview) => void) => subscribe(events.overlayPreview, cb);

export function previewOverlaySelection(session: number, rect: Rect | null): Promise<void> {
  if (!hasNative) return Promise.resolve();
  return Capture.previewOverlaySelection(session, rect);
}
export const onScrollInit = (cb: (payload: ScrollInit) => void) => subscribe(events.scrollInit, cb);
export const onScrollProgress = (cb: (payload: ScrollProgress) => void) => subscribe(events.scrollProgress, (value) => cb({ ...value, status: value.status as ScrollProgress["status"] }));
export const onScrollResult = (cb: (payload: ScrollResult) => void) => subscribe(events.scrollResult, cb);
export const onHistoryChanged = (cb: () => void) => subscribe(events.historyChanged, cb);
export const onToast = (cb: (payload: Native.ToastPayload) => void) => subscribe(events.toast, cb);

// ---------------------------------------------------------------------------
// Browser-preview fallbacks (the design prototype, made safe).
// ---------------------------------------------------------------------------

function browserBeginRegion(): Promise<void> {
  // No overlay windows exist in a browser tab; the main page shows the design's
  // demo screenshot instead (see main.ts).
  return Promise.resolve();
}

function browserCapture(rect: Rect): OverlayCapture {
  // Two device pixels per CSS pixel keeps the demo capture looking Retina-ish.
  return {
    id: "browser-capture",
    url: mockShotDataURL(),
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

// ------------------------------------------------------------
// Browser-preview artwork (no MyGo runtime, no canvas needed)
// ------------------------------------------------------------

/** Deterministic browser-preview screenshot as an SVG data URL. */
function mockShotDataURL(): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="960" height="480" viewBox="0 0 960 480">
<rect width="960" height="480" fill="#f2efe7"/>
<rect width="960" height="40" fill="#e4e0d6"/>
<circle cx="20" cy="20" r="4" fill="#d37d65"/><circle cx="36" cy="20" r="4" fill="#d2ae64"/><circle cx="52" cy="20" r="4" fill="#90ab86"/>
<text x="90" y="25" font-family="system-ui,sans-serif" font-size="12" fill="#6d6c63">团队工作台 / 设计评审</text>
<rect y="40" width="172" height="440" fill="#e8e3d7"/>
<text x="24" y="88" font-family="system-ui,sans-serif" font-size="20" font-weight="700" fill="#343b32">Fieldnotes</text>
<text x="28" y="143" font-family="system-ui,sans-serif" font-size="14" fill="#666a60">今日工作</text>
<text x="28" y="187" font-family="system-ui,sans-serif" font-size="14" fill="#666a60">项目资料</text>
<text x="28" y="231" font-family="system-ui,sans-serif" font-size="14" fill="#666a60">设计参考</text>
<text x="208" y="77" font-family="system-ui,sans-serif" font-size="12" fill="#6a6f63">项目资料  /  品牌更新</text>
<text x="208" y="128" font-family="system-ui,sans-serif" font-size="32" font-weight="700" fill="#303a2e">把想法留在眼前。</text>
<text x="208" y="158" font-family="system-ui,sans-serif" font-size="15" fill="#73776b">Keep your ideas in sight.</text>
<rect x="208" y="190" width="714" height="1" fill="#d7d6c3"/>
<text x="208" y="224" font-family="system-ui,sans-serif" font-size="14" fill="#444d3e">下一步，让每一张截图都有去处。</text>
<text x="208" y="250" font-family="system-ui,sans-serif" font-size="13" fill="#797c71">将参考、标注和讨论放在同一处，继续你的工作。</text>
<rect x="208" y="279" width="214" height="144" rx="10" fill="#dddcc9"/>
<rect x="438" y="279" width="214" height="144" rx="10" fill="#d1d7ca"/>
<rect x="668" y="279" width="254" height="144" rx="10" fill="#e1d3bd"/>
<text x="226" y="306" font-family="system-ui,sans-serif" font-size="13" fill="#6c765d">视觉方向</text>
<text x="456" y="306" font-family="system-ui,sans-serif" font-size="13" fill="#6c765d">材料与细节</text>
<text x="686" y="306" font-family="system-ui,sans-serif" font-size="13" fill="#6c765d">今天的安排</text>
<rect x="226" y="323" width="74" height="68" fill="#818b6f"/>
<rect x="308" y="323" width="96" height="68" fill="#b3b596"/>
<rect x="456" y="323" width="178" height="68" fill="#a4b09b"/>
<text x="686" y="345" font-family="system-ui,sans-serif" font-size="13" fill="#8a7d64">14:00   设计评审</text>
<text x="686" y="380" font-family="system-ui,sans-serif" font-size="13" fill="#8a7d64">16:30   整理反馈</text>
<text x="208" y="457" font-family="system-ui,sans-serif" font-size="11" fill="#777d70">浏览器预览 · 示例截图</text>
</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

/** Browser-preview history thumbnail: a tinted crop-like variant. */
function mockThumbDataURL(variant = 0): string {
  const tints = ["#f3a24e", "#78a576", "#70a8c2", "#e46c4e"];
  const tint = tints[variant % tints.length];
  const shift = (variant % 3) * 42;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="160" viewBox="0 0 320 160">
<rect width="320" height="160" fill="#e8e3d7"/>
<rect width="320" height="26" fill="#d9d4c8"/>
<rect y="26" width="58" height="134" fill="#dcd8cb"/>
<rect x="70" y="44" width="200" height="10" rx="3" fill="${tint}" opacity=".85"/>
<rect x="70" y="64" width="150" height="6" rx="3" fill="#9a968a"/>
<rect x="70" y="80" width="170" height="6" rx="3" fill="#b0ab9d"/>
<rect x="70" y="106" width="70" height="40" rx="6" fill="${tint}" opacity=".5"/>
<rect x="150" y="106" width="70" height="40" rx="6" fill="#b8c0ae"/>
<rect x="230" y="106" width="${44 + shift}px" height="40" rx="6" fill="#d0c3aa"/>
</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

/** Browser-preview history copy: try the real clipboard from the mock image. */
async function browserHistoryCopy(id: string): Promise<void> {
  const entry = mockHistory().find((item) => item.id === id);
  if (!entry) throw new Error(`找不到截图 ${id}`);
  const response = await fetch(entry.url);
  if (!response.ok) throw new Error("图片读取失败");
  const blob = await response.blob();
  if (navigator.clipboard?.write && typeof ClipboardItem !== "undefined") {
    try {
      await navigator.clipboard.write([new ClipboardItem({ "image/png": blob })]);
      return;
    } catch {
      throw new Error("当前浏览器预览未开放图片剪贴板写入");
    }
  }
  throw new Error("当前浏览器预览未开放图片剪贴板写入");
}

/** Browser-preview history save: download the mock image. */
async function browserHistorySave(id: string): Promise<string> {
  const entry = mockHistory().find((item) => item.id === id);
  if (!entry) throw new Error(`找不到截图 ${id}`);
  const response = await fetch(entry.url);
  if (!response.ok) throw new Error("图片读取失败");
  const blob = await response.blob();
  const filename = `Pinstill-history-${id.replace(/[^a-z0-9-]/gi, "") || "image"}.svg`;
  downloadBlob(blob, filename);
  return filename;
}

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
    url: mockShotDataURL(),
    thumbUrl: mockThumbDataURL(index),
  }));
  return mockEntries;
}
