// The region-selection overlay, Round 3: after the drag the selection becomes a
// real in-place annotation surface.
//
// Flow (plan R3.1): drag a selection → `Capture.PrepareSelection(rect)` → Go
// captures through the transparent selection and returns the payload → the
// page renders the captured PNG into
// `#overlayCanvas` inside the selection and shows the COMPLETE annotation
// toolbar (tools, color swatches, width/font slider, text entry, undo,
// 复制/贴图/保存/取消). Actions send the composited PNG through
// `Capture.FinishAnnotated(action, base64)`; Go performs them.
//
// When `afterCapture` is not "inplace" the toolbar is skipped: the raw captured
// PNG bytes go straight to `FinishAnnotated` with the mapped action. In a plain
// browser (no MyGo runtime) the flow is simulated with the design's demo image
// and toasts, so the static preview stays demoable without console errors.

import { DEFAULT_PREFERENCES, loadPreferences } from "./settings";
import { AnnotationEngine, DEFAULT_TEXT_FONT, InlineTextEditor } from "./annotate";
import type { Point, Shape, WatermarkLayer } from "./annotate";
import {
  beginRegion,
  cancelRegion,
  finishAnnotated,
  hasNative,
  onOverlayPreview,
  onOverlayShown,
  openPermissionSettings,
  overlayInfo,
  overlayReady,
  prepareSelection,
  prepareSelectionFast,
  previewOverlaySelection,
  settings,
  startScroll,
  type FinishAnnotatedAction,
  type OverlayCapture,
  type OverlayInfo,
  type OverlayWindow,
  type Rect,
} from "./bridge";
import {
  loadRecentColors,
  normalizeColor,
  PRESET_COLORS,
  rememberColor,
} from "./colors";
import { ColorPalettePanel, createPaletteToggleButton } from "./palette";
import { PrecisionInspector } from "./precision";
import { DRAW_TOOLS, TOOLS, TOOL_ICONS, isDrawTool, type ToolId } from "./tools";
import { blobToBase64, decodeImageBlob, flashButtonSuccess, setButtonPending } from "./util";

const MIN_SELECTION = 3;
const TOOLBAR_GAP = 10;
const EDGE_MARGIN = 8;
// Keep these in sync with internal/imagebudget/budget.go.
const MAX_IMAGE_PIXELS = 32 * 1024 * 1024;
const MAX_ENCODED_BYTES = 64 * 1024 * 1024;

function checkImageDimensions(width: number, height: number): void {
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height) || width <= 0 || height <= 0) {
    throw new Error("图片尺寸无效");
  }
  if (width > MAX_IMAGE_PIXELS / height) throw new Error("图片像素超过上限（32 × 1024²）");
}

type Phase = "idle" | "selecting" | "preparing" | "annotating" | "finishing";
type AfterCapture = "inplace" | "clipboard" | "pin";

/** Toolbar actions as exposed to the page/E2E (data-action). */
type ToolbarAction = FinishAnnotatedAction;

const PENDING_LABELS: Record<ToolbarAction, string> = {
  copy: "正在复制…",
  pin: "正在固定到桌面…",
  hang: "正在挂到桌面晾绳…",
  save: "正在保存…",
  cancel: "正在取消…",
};

const DEMO_MESSAGES: Record<ToolbarAction, string> = {
  copy: "复制：桌面应用中会把合成图写入系统剪贴板",
  pin: "贴图：桌面应用中会创建置顶的桌面贴图窗口",
  hang: "晾图：桌面应用中会把截图挂到桌面晾绳",
  save: "保存：桌面应用中会打开保存对话框",
  cancel: "已取消本次截图",
};

const ACTION_DEFS: Array<{
  action: ToolbarAction;
  label: string;
  icon: string;
  className?: string;
}> = [
  { action: "copy", label: "复制", icon: "copy", className: "primary" },
  { action: "pin", label: "贴图", icon: "pin" },
  { action: "hang", label: "晾图", icon: "images" },
  { action: "save", label: "保存", icon: "download" },
  { action: "cancel", label: "取消", icon: "x", className: "cancel" },
];

/** Editor actions Pinstill does not implement yet: honest "待接入" placeholders. */
const EXTRA_TOOL_DEFS: Array<{
  tool: string;
  label: string;
  icon: string;
  placeholder: boolean;
}> = [
  { tool: "translate", label: "翻译", icon: "languages", placeholder: true },
  { tool: "ocr", label: "提取文字", icon: "scan-text", placeholder: true },
  { tool: "qr", label: "识别二维码", icon: "qr-code", placeholder: true },
  // Real Round 4 action: hands the selection to the scrolling-capture session.
  { tool: "scroll", label: "滚动截图", icon: "scroll-text", placeholder: false },
];

const PLACEHOLDER_TOOLS = new Set(
  EXTRA_TOOL_DEFS.filter((def) => def.placeholder).map((def) => def.tool),
);

// Lucide-style inline icons (the overlay page does not pull in the icon set).
const SVG_PATHS: Record<string, string> = {
  move: '<path d="M12 2v20"/><path d="m8 6 4-4 4 4"/><path d="m8 18 4 4 4-4"/><path d="M2 12h20"/><path d="m6 8-4 4 4 4"/><path d="m18 8 4 4-4 4"/>',
  square: '<rect x="3" y="3" width="18" height="18" rx="2"/>',
  circle: '<circle cx="12" cy="12" r="9"/>',
  minus: '<path d="M5 12h14"/>',
  "arrow-up-right": '<path d="M7 17 17 7"/><path d="M7 7h10v10"/>',
  "pen-line":
    '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
  type: '<path d="M4 7V4h16v3"/><path d="M9 20h6"/><path d="M12 4v16"/>',
  tag: '<path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z"/><circle cx="7.5" cy="7.5" r=".6" fill="currentColor"/>',
  "grid-2x2":
    '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 12h18"/><path d="M12 3v18"/>',
  highlighter:
    '<path d="m9 11-6 6v3h9l3-3"/><path d="m22 12-4.6 4.6a2 2 0 0 1-2.8 0l-5.2-5.2a2 2 0 0 1 0-2.8L14 4"/>',
  stamp:
    '<path d="M5 22h14"/><path d="M19.27 13.73A2.5 2.5 0 0 0 17.5 13h-11A2.5 2.5 0 0 0 4 15.5V17a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-1.5c0-.66-.26-1.3-.73-1.77Z"/><path d="M14 13V8.5C14 7 15 7 15 5a3 3 0 0 0-6 0c0 2 1 2 1 3.5V13"/>',
  "undo-2": '<path d="M9 14 4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11"/>',
  "redo-2": '<path d="m15 14 5-5-5-5"/><path d="M20 9H9.5a5.5 5.5 0 0 0 0 11H13"/>',
  "mouse-pointer-2": '<path d="m4 4 16 7-7 2-2 7L4 4Z"/>',
  copy: '<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
  pin: '<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
  images: '<path d="M18 22H6a2 2 0 0 1-2-2V6"/><rect x="8" y="2" width="12" height="16" rx="2"/><circle cx="12.5" cy="6.5" r="1.5"/><path d="m8 14 3-3 3 3 3-4 3 4"/>',
  download:
    '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M7 10l5 5 5-5"/><path d="M12 15V3"/>',
  "square-pen":
    '<path d="M12 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.375 2.625a2.121 2.121 0 1 1 3 3L12 15l-4 1 1-4Z"/>',
  x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
  languages:
    '<path d="m5 8 6 6"/><path d="m4 14 6-6 2-3"/><path d="M2 5h12"/><path d="M7 2h1"/><path d="m22 22-5-10-5 10"/><path d="M14 18h6"/>',
  "scan-text":
    '<path d="M3 7V5a2 2 0 0 1 2-2h2"/><path d="M17 3h2a2 2 0 0 1 2 2v2"/><path d="M21 17v2a2 2 0 0 1-2 2h-2"/><path d="M7 21H5a2 2 0 0 1-2-2v-2"/><path d="M7 8h8"/><path d="M7 12h10"/><path d="M7 16h6"/>',
  "qr-code":
    '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M14 14h3v3h-3z"/><path d="M18 18h3v3h-3z"/><path d="M14 21h2"/><path d="M21 14v2"/>',
  "scroll-text":
    '<path d="M15 12h-5"/><path d="M15 8h-5"/><path d="M19 17V5a2 2 0 0 0-2-2H4"/><path d="M8 21h12a2 2 0 0 0 2-2v-1a1 1 0 0 0-1-1H11a1 1 0 0 0-1 1v1a2 2 0 1 1-4 0V5a2 2 0 1 0-4 0v2a1 1 0 0 0 1 1h3"/>',
};

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const dim = el("dim");
const maskTop = el("maskTop");
const maskBottom = el("maskBottom");
const maskLeft = el("maskLeft");
const maskRight = el("maskRight");
const selection = el("selection");
const canvas = el<HTMLCanvasElement>("overlayCanvas");
const annotationSelection = el("annotationSelection");
const annotationEditText = el<HTMLButtonElement>("annotationEditText");
const prepareState = el("prepareState");
const crosshairV = el("crosshairV");
const crosshairH = el("crosshairH");
const sizeBadge = el("sizeBadge");
const hint = el("hint");
const toolbar = el("annotationToolbar");
const annotationHandle = el<HTMLButtonElement>("annotationHandle");
const toolsHost = el("overlayTools");
const colorsHost = el("overlayColors");
const actionsHost = el("overlayActions");
const widthInput = el<HTMLInputElement>("overlayWidth");
const widthValue = el("overlayWidthValue");
const widthLabel = el("overlayWidthLabel");
const widthLabelText = el("overlayWidthLabelText");
const textInput = el<HTMLInputElement>("overlayText");
const watermarkProps = el("overlayWatermark");
const watermarkTextInput = el<HTMLInputElement>("watermarkText");
const watermarkOpacityInput = el<HTMLInputElement>("watermarkOpacity");
const watermarkOpacityValue = el("watermarkOpacityValue");
const textProps = el("overlayTextProps");
const textBoldButton = el<HTMLButtonElement>("textBold");
const textItalicButton = el<HTMLButtonElement>("textItalic");
const textSizeSelect = el<HTMLSelectElement>("textSize");
const textFontSelect = el<HTMLSelectElement>("textFont");
const toolbarStatus = el("overlayStatus");
const demoToast = el("demoToast");
const captureError = el("captureError");
const captureErrorTitle = el("captureErrorTitle");
const captureErrorText = el("captureErrorText");
const captureErrorSettings = el<HTMLButtonElement>("captureErrorSettings");
const captureErrorRetry = el<HTMLButtonElement>("captureErrorRetry");
const captureErrorCancel = el<HTMLButtonElement>("captureErrorCancel");
const hintInitialHTML = hint.innerHTML;

let storedPreferences = loadPreferences();
let color =
  normalizeColor(storedPreferences.defaultColor) ?? PRESET_COLORS[0]!;
let penWidth =
  typeof storedPreferences.defaultWidth === "number"
    ? clamp(Math.round(storedPreferences.defaultWidth), 1, 10)
    : 2;
let watermarkText =
  typeof storedPreferences.watermarkText === "string" && storedPreferences.watermarkText
    ? storedPreferences.watermarkText
    : "Pinstill";
// Text annotations used to derive size and weight from the line-width
// slider, which made them heavier than mainstream tools. Size (pt), bold,
// italic and font family are explicit now, and remembered across captures.
interface TextStyleState {
  size: number;
  bold: boolean;
  italic: boolean;
  family: string;
}

const TEXT_STYLE_KEY = "pinstill.textStyle";
const TEXT_SIZES: ReadonlyArray<number> = [10, 11, 12, 14, 16, 18, 20, 24, 28, 32];
const TEXT_FONTS: ReadonlyArray<Readonly<{ value: string; label: string }>> = [
  { value: DEFAULT_TEXT_FONT, label: "系统默认" },
  { value: '"PingFang SC", -apple-system, sans-serif', label: "苹方" },
  { value: '"Helvetica Neue", Helvetica, sans-serif', label: "Helvetica" },
  { value: 'Georgia, "Songti SC", serif', label: "Georgia" },
  { value: 'Menlo, "SF Mono", monospace', label: "Menlo" },
];
const DEFAULT_TEXT_STYLE: TextStyleState = {
  size: 14,
  bold: false,
  italic: false,
  family: DEFAULT_TEXT_FONT,
};

function loadTextStyle(): TextStyleState {
  try {
    const raw = JSON.parse(localStorage.getItem(TEXT_STYLE_KEY) ?? "null") as
      | Partial<TextStyleState>
      | null;
    if (!raw || typeof raw !== "object") return { ...DEFAULT_TEXT_STYLE };
    const size = TEXT_SIZES.includes(Number(raw.size)) ? Number(raw.size) : DEFAULT_TEXT_STYLE.size;
    const family = TEXT_FONTS.some((font) => font.value === raw.family)
      ? String(raw.family)
      : DEFAULT_TEXT_STYLE.family;
    return { size, bold: Boolean(raw.bold), italic: Boolean(raw.italic), family };
  } catch {
    return { ...DEFAULT_TEXT_STYLE };
  }
}

let textStyle: TextStyleState = loadTextStyle();

function saveTextStyle(): void {
  try {
    localStorage.setItem(TEXT_STYLE_KEY, JSON.stringify(textStyle));
  } catch {
    // Storage may be unavailable; the session state still applies.
  }
}


let watermarkOpacity =
  typeof storedPreferences.watermarkOpacity === "number"
    ? clamp(storedPreferences.watermarkOpacity, 10, 90)
    : 35;

const initialTool = storedPreferences.defaultTool;
let currentTool: ToolId =
  typeof initialTool === "string" && isDrawTool(initialTool) ? initialTool : DEFAULT_PREFERENCES.defaultTool as ToolId;
let tooltipsEnabled = storedPreferences.tooltip !== false;
let tooltipDelayMs =
  typeof storedPreferences.tooltipDelay === "number"
    ? clamp(Math.round(storedPreferences.tooltipDelay), 0, 2000)
    : 120;
let enterCopyEnabled = storedPreferences.enterCopy !== false;
let entry = "重点说明";

// Tiled watermark: a full-canvas layer (not a movable shape) with its own
// text and opacity, remembered across captures. The colour follows the
// current annotation colour and the size follows the 字号 slider.
interface WatermarkSettings {
  text: string;
  opacity: number;
  generalText: string;
  generalOpacity: number;
}

const WATERMARK_STYLE_KEY = "pinstill.watermark";

function loadWatermarkSettings(): WatermarkSettings {
  const fallback: WatermarkSettings = {
    text: watermarkText,
    opacity: clamp(watermarkOpacity, 10, 90),
    generalText: watermarkText,
    generalOpacity: clamp(watermarkOpacity, 10, 90),
  };
  try {
    const raw = JSON.parse(localStorage.getItem(WATERMARK_STYLE_KEY) ?? "null") as
      | Partial<WatermarkSettings>
      | null;
    // Older toolbar values have no baseline: retain them once and attach the
    // current defaults. Future general text/opacity changes take precedence.
    const legacy = raw && raw.generalText === undefined && raw.generalOpacity === undefined;
    if (raw && typeof raw.text === "string" && (legacy ||
      (raw.generalText === fallback.generalText && raw.generalOpacity === fallback.generalOpacity))) {
      fallback.text = raw.text.slice(0, 20);
      fallback.opacity = clamp(Math.round(Number(raw.opacity) || 30), 10, 90);
    }
    localStorage.setItem(WATERMARK_STYLE_KEY, JSON.stringify(fallback));
  } catch {
    // Ignore broken storage and use the settings defaults.
  }
  return fallback;
}

let watermark = loadWatermarkSettings();
let watermarkEnabled = false;

function watermarkLayer(): WatermarkLayer | null {
  if (!watermarkEnabled) return null;
  const text = watermark.text.trim();
  if (!text) return null;
  return {
    text,
    opacity: watermark.opacity,
    // Reuse the line-width slider as the watermark font size.
    size: penWidth * 3 + 18,
    color,
  };
}

function applyWatermark(): void {
  engine?.setWatermark(watermarkLayer());
}

function saveWatermark(): void {
  try {
    localStorage.setItem(WATERMARK_STYLE_KEY, JSON.stringify(watermark));
  } catch {
    // Storage may be unavailable; the session state still applies.
  }
}

function updateWatermarkUI(): void {
  watermarkTextInput.value = watermark.text;
  watermarkOpacityInput.value = String(watermark.opacity);
  watermarkOpacityValue.textContent = `${watermark.opacity}%`;
}


// Tooltip preferences are read from localStorage above; apply them only now,
// otherwise `tooltipsEnabled` is still undefined when the init code runs.
document.documentElement.style.setProperty("--tooltip-delay", `${tooltipDelayMs}ms`);
if (!tooltipsEnabled) toolbar.classList.add("no-tooltips");

let displayInfo: OverlayInfo | null = null;
let phase: Phase = "idle";
let afterCapture: AfterCapture = "inplace";
let settingsReady: Promise<void> = Promise.resolve();
let detectedWindows: OverlayWindow[] = [];
let pressWindow: OverlayWindow | null = null;
let dragMoved = false;
let overlayShownHandled = false;
let passiveSelection = false;
let peerPreview: { session: number; rect: Rect | null } | null = null;
let peerPreviewPending = false;

let startX = 0;
let startY = 0;
let currentX = 0;
let currentY = 0;
let endpointOffsetX = 0;
let endpointOffsetY = 0;
let committedRect: Rect | null = null;
let keyboardAdjustment: { original: Rect; rect: Rect; timer: number } | null = null;
const precision = new PrecisionInspector(
  () => ({ width: window.innerWidth, height: window.innerHeight }),
  (message) => showTransient(message),
);

let engine: AnnotationEngine | null = null;
let displayedAnnotation: Readonly<Shape> | null = null;
let captureAttempt = 0;
let captureImage: ImageBitmap | HTMLImageElement | HTMLCanvasElement | null = null;
let settingsSequence = 0;
let drawingPointer: number | null = null;

let transientTimer: number | undefined;
let demoResetTimer: number | undefined;
let recentColors: string[] = loadRecentColors();
let inlineEditor: InlineTextEditor | null = null;

// The shared in-app palette panel (R7.8). `setColor` is a hoisted function
// declaration, so it is available when a colour is picked later.
const palettePanel = new ColorPalettePanel({
  getColor: () => selectedAnnotation()?.color ?? color,
  onColor: (next, options) => {
    if (options.remember || !selectedAnnotation()) setColor(next, options.remember);
  },
});

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  return "未知错误";
}

function viewportWidth(): number {
  return displayInfo?.width && displayInfo.width > 0 ? displayInfo.width : window.innerWidth;
}

function viewportHeight(): number {
  return displayInfo?.height && displayInfo.height > 0 ? displayInfo.height : window.innerHeight;
}

function desktopBounds(): Rect {
  return displayInfo?.desktop ?? { x: 0, y: 0, width: viewportWidth(), height: viewportHeight() };
}

function editingRect(rect: Rect): Rect {
  const width = viewportWidth();
  const height = viewportHeight();
  if (rect.x >= 0 && rect.y >= 0 && rect.x + rect.width <= width && rect.y + rect.height <= height) return rect;
  const scale = Math.min(1, Math.max(1, width - 48) / rect.width, Math.max(1, height - 200) / rect.height);
  const fittedWidth = rect.width * scale;
  const fittedHeight = rect.height * scale;
  return { x: (width - fittedWidth) / 2, y: (height - fittedHeight) / 2, width: fittedWidth, height: fittedHeight };
}

function toolLabel(tool: ToolId): string {
  return TOOLS.find(([id]) => id === tool)?.[1] ?? tool;
}

function svgIcon(name: string): string {
  const paths = SVG_PATHS[name] ?? SVG_PATHS.square ?? "";
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
}

function isInsideToolbar(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("#annotationToolbar"));
}

function isInsidePalette(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("#colorPalettePanel"));
}

function isInsideTextEditor(target: EventTarget | null): boolean {
  return (
    target instanceof Element &&
    Boolean(target.closest(".inline-text-input, #textInputOverlay"))
  );
}

// ---------------------------------------------------------------------------
// Selection geometry
// ---------------------------------------------------------------------------

function selectionRect(): Rect {
  return {
    x: Math.min(startX, currentX),
    y: Math.min(startY, currentY),
    width: Math.abs(currentX - startX),
    height: Math.abs(currentY - startY),
  };
}

function roundedSelectionRect(): Rect {
  const rect = selectionRect();
  const desktop = desktopBounds();
  const right = desktop.x + desktop.width;
  const bottom = desktop.y + desktop.height;
  const x = clamp(Math.round(rect.x), desktop.x, right);
  const y = clamp(Math.round(rect.y), desktop.y, bottom);
  return {
    x,
    y,
    width: Math.max(1, Math.min(Math.round(rect.x + rect.width), right) - x),
    height: Math.max(1, Math.min(Math.round(rect.y + rect.height), bottom) - y),
  };
}

function moveCrosshair(x: number, y: number): void {
  precision.move(x, y);
  crosshairV.hidden = false;
  crosshairH.hidden = false;
  crosshairV.style.left = `${x}px`;
  crosshairH.style.top = `${y}px`;
}

function updateSizeBadge(rect: Rect, toolbarAbove = false, label?: string, badgeAnchor = rect): void {
  const dims = `${Math.round(rect.width)} × ${Math.round(rect.height)}`;
  const cleanLabel = label?.trim().slice(0, 24) ?? "";
  sizeBadge.textContent = cleanLabel ? `${cleanLabel} · ${dims}` : dims;
  sizeBadge.hidden = false;
  const badgeWidth = sizeBadge.offsetWidth || 84;
  const badgeHeight = sizeBadge.offsetHeight || 24;
  let left = badgeAnchor.x + badgeAnchor.width - badgeWidth;
  let top = toolbarAbove ? badgeAnchor.y + 8 : badgeAnchor.y - badgeHeight - 8;
  if (top < EDGE_MARGIN) top = badgeAnchor.y + 8;
  left = clamp(left, EDGE_MARGIN, Math.max(EDGE_MARGIN, viewportWidth() - badgeWidth - EDGE_MARGIN));
  top = clamp(top, EDGE_MARGIN, Math.max(EDGE_MARGIN, viewportHeight() - badgeHeight - EDGE_MARGIN));
  sizeBadge.style.left = `${left}px`;
  sizeBadge.style.top = `${top}px`;
}

// Four cheap panels darken everything outside the selection. They replace a
// single huge box-shadow, which made dragging/resizing repaint the whole
// screen and feel laggy.
function positionMasks(rect: Rect | null): void {
  const vw = viewportWidth();
  const vh = viewportHeight();
  if (!rect) {
    maskTop.hidden = true;
    maskBottom.hidden = true;
    maskLeft.hidden = true;
    maskRight.hidden = true;
    return;
  }
  const x = clamp(rect.x, 0, vw);
  const y = clamp(rect.y, 0, vh);
  const w = clamp(rect.x + rect.width, x, vw) - x;
  const h = clamp(rect.y + rect.height, y, vh) - y;
  const place = (el: HTMLElement, left: number, top: number, width: number, height: number) => {
    const visible = width > 0.5 && height > 0.5;
    el.hidden = !visible;
    if (!visible) return;
    el.style.left = `${left}px`;
    el.style.top = `${top}px`;
    el.style.width = `${width}px`;
    el.style.height = `${height}px`;
  };
  place(maskTop, 0, 0, vw, y);
  place(maskBottom, 0, y + h, vw, vh - (y + h));
  place(maskLeft, 0, y, x, h);
  place(maskRight, x + w, y, vw - (x + w), h);
  dim.hidden = true;
}

function renderSelection(): void {
  const rect = selectionRect();
  positionSelection(rect);
  updateSizeBadge(rect);
  queuePeerPreview(rect);
}

function positionSelection(rect: Rect): void {
  selection.hidden = false;
  positionMasks(rect);
  selection.style.left = `${rect.x}px`;
  selection.style.top = `${rect.y}px`;
  selection.style.width = `${rect.width}px`;
  selection.style.height = `${rect.height}px`;
}

function queuePeerPreview(rect: Rect | null): void {
  const session = displayInfo?.session;
  if (!displayInfo?.desktop || !session) return;
  peerPreview = { session, rect };
  if (peerPreviewPending) return;
  peerPreviewPending = true;
  requestAnimationFrame(publishPeerPreview);
}

function publishPeerPreview(): void {
  const pending = peerPreview;
  peerPreview = null;
  if (!pending || pending.session !== displayInfo?.session) {
    peerPreviewPending = false;
    return;
  }
  void previewOverlaySelection(pending.session, pending.rect).catch(() => {}).finally(() => {
    if (peerPreview) requestAnimationFrame(publishPeerPreview);
    else peerPreviewPending = false;
  });
}

function sanitizeWindows(list: OverlayWindow[] | undefined): OverlayWindow[] {
  if (!Array.isArray(list)) return [];
  return list.filter(
    (w): w is OverlayWindow =>
      Boolean(w) &&
      Number.isFinite(w.x) &&
      Number.isFinite(w.y) &&
      Number.isFinite(w.width) &&
      Number.isFinite(w.height) &&
      w.width >= MIN_SELECTION &&
      w.height >= MIN_SELECTION,
  );
}

function clampWindowRect(win: OverlayWindow): Rect {
  const vw = viewportWidth();
  const vh = viewportHeight();
  const x = clamp(Math.round(win.x), 0, vw);
  const y = clamp(Math.round(win.y), 0, vh);
  const right = clamp(Math.round(win.x + win.width), x, vw);
  const bottom = clamp(Math.round(win.y + win.height), y, vh);
  return {
    x,
    y,
    width: Math.max(0, right - x),
    height: Math.max(0, bottom - y),
  };
}

function findWindowAt(x: number, y: number): OverlayWindow | null {
  for (const win of detectedWindows) {
    if (
      x >= win.x &&
      x < win.x + win.width &&
      y >= win.y &&
      y < win.y + win.height
    ) {
      return win;
    }
  }
  return null;
}

function showWindowPreview(win: OverlayWindow | null): void {
  if (win) {
    const rect = clampWindowRect(win);
    if (rect.width >= MIN_SELECTION && rect.height >= MIN_SELECTION) {
      selection.classList.add("window-preview");
      positionSelection(rect);
      updateSizeBadge(rect, false, win.owner);
      return;
    }
  }
  selection.classList.remove("window-preview");
  selection.hidden = true;
  positionMasks(null);
  dim.hidden = false;
  sizeBadge.hidden = true;
}

function updateHoveredWindow(x: number, y: number): void {
  if (phase !== "idle") return;
  showWindowPreview(findWindowAt(x, y));
}

function applyOverlayInfo(info: OverlayInfo | null): void {
  if (info && Number.isFinite(info.width) && info.width > 0) displayInfo = info;
  detectedWindows = sanitizeWindows(info?.windows);
  if (phase !== "idle") return;
  if (
    info?.hasCursor &&
    typeof info.cursorX === "number" &&
    typeof info.cursorY === "number" &&
    Number.isFinite(info.cursorX) &&
    Number.isFinite(info.cursorY)
  ) {
    const cx = clamp(info.cursorX, 0, viewportWidth());
    const cy = clamp(info.cursorY, 0, viewportHeight());
    moveCrosshair(cx, cy);
    updateHoveredWindow(cx, cy);
  } else {
    showWindowPreview(null);
  }
}

// ---------------------------------------------------------------------------
// Transient feedback
// ---------------------------------------------------------------------------

function showTransient(message: string, timeout = 2600): void {
  demoToast.textContent = message;
  demoToast.hidden = false;
  if (transientTimer) window.clearTimeout(transientTimer);
  transientTimer = window.setTimeout(() => {
    demoToast.hidden = true;
    transientTimer = undefined;
  }, timeout);
}

function setToolbarStatus(text: string, kind: "info" | "error" = "info"): void {
  toolbarStatus.textContent = text;
  toolbarStatus.dataset.kind = kind;
}

// ---------------------------------------------------------------------------
// Toolbar
// ---------------------------------------------------------------------------

function buildToolbar(): void {
  toolsHost.replaceChildren();
  for (const tool of ["select", ...DRAW_TOOLS, "undo", "redo"] as const) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tool-btn";
    button.dataset.tool = tool;
    button.setAttribute("aria-label", toolLabel(tool));
    if (tool === "undo" || tool === "redo") button.disabled = true;
    else button.setAttribute("aria-pressed", "false");
    const tip = tool === "select" ? `${toolLabel(tool)} (V)` : toolLabel(tool);
    button.innerHTML = `${svgIcon(TOOL_ICONS[tool] ?? "square")}<span class="tip">${tip}</span>`;
    toolsHost.append(button);
  }

  const actionSeparator = document.createElement("span");
  actionSeparator.className = "at-tool-sep";
  actionSeparator.setAttribute("aria-hidden", "true");
  toolsHost.append(actionSeparator);
  for (const def of EXTRA_TOOL_DEFS) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tool-btn";
    button.dataset.tool = def.tool;
    button.setAttribute("aria-label", def.label);
    button.title = def.placeholder ? `${def.label}（待接入）` : def.label;
    button.innerHTML = `${svgIcon(def.icon)}<span class="tip">${def.label}</span>`;
    toolsHost.append(button);
  }

  renderPalette();

  textSizeSelect.replaceChildren();
  for (const size of TEXT_SIZES) {
    const option = document.createElement("option");
    option.value = String(size);
    option.textContent = `${size}pt`;
    textSizeSelect.append(option);
  }
  textFontSelect.replaceChildren();
  for (const font of TEXT_FONTS) {
    const option = document.createElement("option");
    option.value = font.value;
    option.textContent = font.label;
    textFontSelect.append(option);
  }
  textBoldButton.addEventListener("click", () => {
    editTextStyle({ bold: !activeTextStyle().bold });
  });
  textItalicButton.addEventListener("click", () => {
    editTextStyle({ italic: !activeTextStyle().italic });
  });
  textSizeSelect.addEventListener("change", () => {
    const size = Number(textSizeSelect.value);
    editTextStyle({ size: TEXT_SIZES.includes(size) ? size : DEFAULT_TEXT_STYLE.size });
  });
  textFontSelect.addEventListener("change", () => {
    const family = TEXT_FONTS.some((font) => font.value === textFontSelect.value)
      ? textFontSelect.value
      : DEFAULT_TEXT_STYLE.family;
    editTextStyle({ family });
  });
  annotationEditText.addEventListener("click", editSelectedText);
  applyTextStyleUI();

  actionsHost.replaceChildren();
  for (const def of ACTION_DEFS) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `action-btn${def.className ? ` ${def.className}` : ""}`;
    button.dataset.action = def.action;
    button.title = def.label;
    button.innerHTML = `${svgIcon(def.icon)}<span>${def.label}</span>`;
    actionsHost.append(button);
  }

  toolbar.addEventListener("click", onToolbarClick);
  widthInput.addEventListener("input", () => {
    const width = clamp(Number(widthInput.value) || 1, 1, 10);
    widthValue.textContent = String(width);
    if (!selectedAnnotation()) {
      penWidth = width;
      if (watermarkEnabled) applyWatermark();
    }
  });
  widthInput.addEventListener("change", () => {
    engine?.editSelectedShape({ width: clamp(Number(widthInput.value) || 1, 1, 10) });
  });
  textInput.addEventListener("input", () => {
    entry = textInput.value;
  });
  updateWatermarkUI();
  watermarkTextInput.addEventListener("input", () => {
    watermark.text = watermarkTextInput.value.slice(0, 20);
    saveWatermark();
    applyWatermark();
  });
  watermarkOpacityInput.addEventListener("input", () => {
    watermark.opacity = clamp(Math.round(Number(watermarkOpacityInput.value) || 30), 10, 90);
    watermarkOpacityValue.textContent = `${watermark.opacity}%`;
    saveWatermark();
    applyWatermark();
  });

  selectTool(currentTool);
  setColor(color);
  updateWidthValue();
}

function onToolbarClick(event: Event): void {
  const target = event.target;
  if (!(target instanceof Element)) return;
  if (phase !== "annotating") return;
  const toolButton = target.closest<HTMLButtonElement>("button[data-tool]");
  if (toolButton) {
    const tool = toolButton.dataset.tool;
    if (tool === "undo") undo();
    else if (tool === "redo") redo();
    else if (tool === "scroll") void startScrollFromToolbar();
    else if (tool === "watermark") toggleWatermarkTool();
    else if (tool && PLACEHOLDER_TOOLS.has(tool)) showRecognitionPlaceholder(tool);
    else if (tool) selectTool(tool as ToolId);
    return;
  }
  const actionButton = target.closest<HTMLButtonElement>("button[data-action]");
  if (actionButton) {
    const action = actionButton.dataset.action as ToolbarAction | undefined;
    if (action) void runAction(action);
    return;
  }
  const swatchButton = target.closest<HTMLElement>(".swatch");
  if (swatchButton?.dataset.color) setColor(swatchButton.dataset.color);
}

/** Scrolling capture: hand the selection to Go. Go hides this overlay, shows
 * the small control-bar window (`/scroll.html`) and starts the capture loop;
 * the next BeginRegion's overlay:shown resets this page. */
async function startScrollFromToolbar(): Promise<void> {
  if (keyboardAdjustment && !await settleKeyboardAdjustment()) return;
  if (phase !== "annotating" || !committedRect) return;
  if (!hasNative) {
    setToolbarStatus("滚动截图 · 浏览器预览", "info");
    showTransient(
      "浏览器预览 · 桌面应用中可以手动滚动，或在控制条里开启自动滚动",
      3400,
    );
    return;
  }
  const attempt = captureAttempt;
  phase = "finishing";
  setToolbarPending(true, "正在进入滚动截图…");
  try {
    await startScroll(committedRect);
    if (!currentAttempt(attempt)) return;
    detectedWindows = [];
    reset();
  } catch (error) {
    if (!currentAttempt(attempt)) return;
    phase = "annotating";
    setToolbarPending(false);
    setToolbarStatus(`无法开始滚动截图：${errorMessage(error)}`, "error");
  }
}

/** Honest "待接入" feedback for editor capabilities Pinstill does not implement
 * yet (translate/OCR/QR/scroll capture). Never calls FinishAnnotated and never
 * touches the annotations. */
function showRecognitionPlaceholder(tool: string): void {
  const def = EXTRA_TOOL_DEFS.find((entry) => entry.tool === tool);
  if (!def || phase !== "annotating") return;
  setToolbarStatus(`${def.label} · 待接入`, "info");
  showTransient(`${def.label} · 待接入：桌面版接入前，可先复制/保存当前截图`, 3200);
  window.setTimeout(() => {
    if (phase === "annotating" && toolbarStatus.textContent === `${def.label} · 待接入`) {
      setToolbarStatus("");
    }
  }, 3200);
}

function selectedAnnotation(): Readonly<Shape> | null {
  return currentTool === "select" ? engine?.getSelectedShape() ?? null : null;
}

function activeTextStyle(): TextStyleState {
  const shape = selectedAnnotation();
  if (shape?.kind !== "text") return textStyle;
  return {
    size: shape.fontSize ?? DEFAULT_TEXT_STYLE.size,
    bold: Boolean(shape.bold),
    italic: Boolean(shape.italic),
    family: shape.fontFamily ?? DEFAULT_TEXT_STYLE.family,
  };
}

function editTextStyle(change: Partial<TextStyleState>): void {
  if (selectedAnnotation()?.kind === "text") {
    const { size, family, ...flags } = change;
    engine?.editSelectedShape({
      ...flags,
      ...(size === undefined ? {} : { fontSize: size }),
      ...(family === undefined ? {} : { fontFamily: family }),
    });
  } else {
    Object.assign(textStyle, change);
    saveTextStyle();
  }
  applyTextStyleUI();
}

function applyTextStyleUI(): void {
  const style = activeTextStyle();
  textBoldButton.setAttribute("aria-pressed", String(style.bold));
  textItalicButton.setAttribute("aria-pressed", String(style.italic));
  textSizeSelect.value = String(style.size);
  textFontSelect.value = style.family;
}

/** The watermark button is an on/off layer: clicking it shows the tiled
 * watermark and its settings row, clicking it again removes the watermark and
 * returns to the rectangle tool. */
function toggleWatermarkTool(): void {
  if (watermarkEnabled) {
    watermarkEnabled = false;
    applyWatermark();
    watermarkProps.hidden = true;
    selectTool("rect");
    return;
  }
  watermarkEnabled = true;
  selectTool("watermark");
}

function selectTool(tool: ToolId): void {
  if (currentTool !== tool) lastCanvasClick = null;
  currentTool = tool;
  engine?.setTool(tool);
  for (const button of toolbar.querySelectorAll<HTMLButtonElement>("button[data-tool]")) {
    if (button.dataset.tool === "undo" || button.dataset.tool === "redo") continue;
    const active = button.dataset.tool === tool;
    button.classList.toggle("active", active);
    button.setAttribute("aria-pressed", String(active));
  }
  // Text and tag use the inline editor at the clicked position, so the old
  // toolbar text field stays hidden; the watermark has its own settings row.
  textInput.hidden = true;
  const isWatermarkTool = tool === "watermark";
  watermarkProps.hidden = !isWatermarkTool;
  widthLabelText.textContent = isWatermarkTool ? "字号" : "粗细";
  if (isWatermarkTool) {
    watermarkEnabled = true;
    updateWatermarkUI();
    applyWatermark();
  }
  canvas.style.cursor = tool === "select" ? "default" : tool === "text" ? "text" : "crosshair";
  updateToolbarState();
  updateAnnotationProperties();
}

function renderPalette(): void {
  colorsHost.replaceChildren();
  for (const preset of PRESET_COLORS) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "swatch";
    button.dataset.color = preset;
    button.style.setProperty("--swatch", preset);
    button.setAttribute("aria-label", `颜色 ${preset}`);
    button.setAttribute("aria-pressed", "false");
    button.addEventListener("click", () => setColor(preset));
    colorsHost.append(button);
  }

  // R7.8: the old "+" custom swatch is now the palette-panel trigger; it keeps
  // `data-color-custom` so earlier hooks stay valid.
  const custom = createPaletteToggleButton("swatch custom");
  custom.dataset.colorCustom = "true";
  custom.addEventListener("click", () => palettePanel.toggle(custom));
  colorsHost.append(custom);

  // The quick row stays compact; the panel shows all MAX_RECENT_COLORS.
  for (const recent of recentColors.slice(0, 4)) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "swatch";
    button.dataset.colorRecent = recent;
    button.style.setProperty("--swatch", recent);
    button.setAttribute("aria-label", `最近使用颜色 ${recent}`);
    button.setAttribute("aria-pressed", "false");
    button.addEventListener("click", () => setColor(recent));
    colorsHost.append(button);
  }
  updatePaletteSelection();
}

function updatePaletteSelection(): void {
  const current = (selectedAnnotation()?.color ?? color).toLowerCase();
  for (const button of colorsHost.querySelectorAll<HTMLElement>(".swatch")) {
    // The palette trigger is a button, not a colour choice.
    if (button.hasAttribute("data-palette-toggle")) continue;
    let selected = false;
    if (button.dataset.color) selected = button.dataset.color.toLowerCase() === current;
    else if (button.dataset.colorRecent) {
      selected = button.dataset.colorRecent.toLowerCase() === current;
    }
    button.classList.toggle("selected", selected);
    button.setAttribute("aria-pressed", String(selected));
  }
}

function setColor(next: string, remember = false): void {
  const normalized = normalizeColor(next);
  if (!normalized) return;
  const selected = selectedAnnotation();
  if (selected) engine?.editSelectedShape({ color: normalized });
  else color = normalized;
  if (remember) {
    recentColors = rememberColor(normalized);
    renderPalette();
  } else {
    updatePaletteSelection();
  }
  if (inlineEditor) inlineEditor.element.style.color = normalized;
  if (watermarkEnabled && !selected) applyWatermark();
}

function updateWidthValue(): void {
  const width = String(selectedAnnotation()?.width ?? penWidth);
  widthInput.value = width;
  widthValue.textContent = width;
}

function updateToolbarState(): void {
  const undoButton = toolbar.querySelector<HTMLButtonElement>('button[data-tool="undo"]');
  const redoButton = toolbar.querySelector<HTMLButtonElement>('button[data-tool="redo"]');
  if (undoButton) undoButton.disabled = !engine?.canUndo();
  if (redoButton) redoButton.disabled = !engine?.canRedo();
  const shape = selectedAnnotation();
  if (shape !== displayedAnnotation) {
    displayedAnnotation = shape;
    updateAnnotationProperties();
  }
  const bounds = shape && !toolbar.hidden ? engine?.getSelectionBounds() : null;
  annotationSelection.hidden = !bounds || !canvas.width || !canvas.height;
  if (bounds && !annotationSelection.hidden) {
    const left = clamp(bounds.x, 0, canvas.width);
    const top = clamp(bounds.y, 0, canvas.height);
    const right = clamp(bounds.x + bounds.width, left, canvas.width);
    const bottom = clamp(bounds.y + bounds.height, top, canvas.height);
    annotationSelection.hidden = right <= left || bottom <= top;
    annotationSelection.style.left = `${left * 100 / canvas.width}%`;
    annotationSelection.style.top = `${top * 100 / canvas.height}%`;
    annotationSelection.style.width = `${(right - left) * 100 / canvas.width}%`;
    annotationSelection.style.height = `${(bottom - top) * 100 / canvas.height}%`;
  }
}

function updateAnnotationProperties(): void {
  const shape = selectedAnnotation();
  const showTextStyle = currentTool === "text" || shape?.kind === "text";
  const showEdit = shape?.kind === "text" || shape?.kind === "tag";
  const needsLayout = textProps.hidden === showTextStyle || annotationEditText.hidden === showEdit;
  textProps.hidden = !showTextStyle;
  annotationEditText.hidden = !showEdit;
  widthLabel.hidden = showTextStyle || (currentTool === "select" && !shape);
  updatePaletteSelection();
  updateWidthValue();
  applyTextStyleUI();
  if (needsLayout && phase === "annotating" && !toolbar.hidden && !selectionResize) placeToolbar();
}

function setToolbarPending(pending: boolean, label = "", kind: "info" | "error" = "info"): void {
  toolbar.setAttribute("aria-busy", String(pending));
  for (const button of toolbar.querySelectorAll<HTMLButtonElement>("button")) {
    button.disabled = pending;
  }
  if (label) setToolbarStatus(label, kind);
  if (!pending) updateToolbarState();
}

function placeToolbar(): void {
  if (!committedRect) return;
  toolbar.hidden = false;
  if (toolbarMoved) return;
  toolbar.style.left = "0px";
  toolbar.style.top = "0px";
  const rect = editingRect(committedRect);
  toolbar.classList.toggle(
    "compact",
    viewportWidth() < 760 || rect.width < 320,
  );
  const toolbarWidth = toolbar.offsetWidth;
  const toolbarHeight = toolbar.offsetHeight;
  const vw = viewportWidth();
  const vh = viewportHeight();
  const below = rect.y + rect.height + TOOLBAR_GAP;
  const above = rect.y - toolbarHeight - TOOLBAR_GAP;
  let toolbarAbove = false;
  let top: number;
  if (below + toolbarHeight <= vh - EDGE_MARGIN) {
    top = below;
  } else if (above >= EDGE_MARGIN) {
    top = above;
    toolbarAbove = true;
  } else {
    top = clamp(below, EDGE_MARGIN, Math.max(EDGE_MARGIN, vh - toolbarHeight - EDGE_MARGIN));
    toolbarAbove = top < rect.y;
  }
  top = clamp(top, EDGE_MARGIN, Math.max(EDGE_MARGIN, vh - toolbarHeight - EDGE_MARGIN));
  const left = clamp(
    rect.x + rect.width / 2 - toolbarWidth / 2,
    EDGE_MARGIN,
    Math.max(EDGE_MARGIN, vw - toolbarWidth - EDGE_MARGIN),
  );
  toolbar.style.left = `${left}px`;
  toolbar.style.top = `${top}px`;
  updateSizeBadge(committedRect, toolbarAbove, undefined, rect);
}

function showToolbar(): void {
  toolbar.hidden = false;
  selection.classList.add("resizable");
  setToolbarPending(false);
  setToolbarStatus("");
  selectTool(currentTool);
  updateToolbarState();
  placeToolbar();
}

/** Clears pending badges left over by an action that a new session replaced. */
function clearActionFeedback(): void {
  for (const button of toolbar.querySelectorAll<HTMLButtonElement>("button.is-pending")) {
    setButtonPending(button, false);
  }
}

function hideToolbar(): void {
  toolbar.hidden = true;
  selection.classList.remove("resizable");
  palettePanel.close();
  clearActionFeedback();
  setToolbarPending(false);
  setToolbarStatus("");
}

// The annotation toolbar can be dragged out of the way: grab the handle (or
// any non-interactive part of the bar) and move it; the position is kept for
// the current capture and the bar is re-placed for the next one.
interface ToolbarDragState {
  pointerId: number;
  startX: number;
  startY: number;
  left: number;
  top: number;
}

let toolbarDrag: ToolbarDragState | null = null;
let toolbarMoved = false;

// Double-clicking the captured image finishes the capture with a copy, like
// the mainstream tools: the action button is a shortcut, not a requirement.
interface CanvasClickState {
  time: number;
  x: number;
  y: number;
}

let lastCanvasClick: CanvasClickState | null = null;

const RESIZE_DIRECTIONS = ["n", "s", "w", "e", "nw", "ne", "sw", "se"] as const;
type ResizeDirection = (typeof RESIZE_DIRECTIONS)[number];

interface SelectionResizeState {
  direction: ResizeDirection;
  pointerId: number;
  startX: number;
  startY: number;
  rect: Rect;
}

function sameRect(a: Rect, b: Rect): boolean {
  return (
    Math.abs(a.x - b.x) < 0.5 &&
    Math.abs(a.y - b.y) < 0.5 &&
    Math.abs(a.width - b.width) < 0.5 &&
    Math.abs(a.height - b.height) < 0.5
  );
}

const MIN_RESIZE = 24;
let selectionResize: SelectionResizeState | null = null;

function startToolbarDrag(event: PointerEvent): void {
  if (toolbar.hidden || event.button !== 0) return;
  const rect = toolbar.getBoundingClientRect();
  toolbarDrag = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    left: rect.left,
    top: rect.top,
  };
  toolbar.style.left = `${rect.left}px`;
  toolbar.style.top = `${rect.top}px`;
  toolbar.classList.add("dragging");
  document.body.classList.add("dragging-toolbar");
  event.preventDefault();
}

function moveToolbarDrag(event: PointerEvent): boolean {
  if (!toolbarDrag || event.pointerId !== toolbarDrag.pointerId) return false;
  const width = toolbar.offsetWidth;
  const height = toolbar.offsetHeight;
  const left = clamp(
    toolbarDrag.left + event.clientX - toolbarDrag.startX,
    EDGE_MARGIN,
    Math.max(EDGE_MARGIN, viewportWidth() - width - EDGE_MARGIN),
  );
  const top = clamp(
    toolbarDrag.top + event.clientY - toolbarDrag.startY,
    EDGE_MARGIN,
    Math.max(EDGE_MARGIN, viewportHeight() - height - EDGE_MARGIN),
  );
  if (left !== toolbarDrag.left || top !== toolbarDrag.top) toolbarMoved = true;
  toolbar.style.left = `${left}px`;
  toolbar.style.top = `${top}px`;
  event.preventDefault();
  return true;
}

function endToolbarDrag(event: PointerEvent): void {
  if (!toolbarDrag || event.pointerId !== toolbarDrag.pointerId) return;
  toolbarDrag = null;
  toolbar.classList.remove("dragging");
  document.body.classList.remove("dragging-toolbar");
  if (committedRect) {
    const rect = editingRect(committedRect);
    updateSizeBadge(committedRect, toolbar.getBoundingClientRect().top < rect.y, undefined, rect);
  }
}

annotationHandle.addEventListener("pointerdown", (event) => {
  startToolbarDrag(event);
  event.stopPropagation();
});
toolbar.addEventListener("pointerdown", (event) => {
  if (event.target instanceof Element && event.target.closest("button,input,label,output,select,textarea")) {
    return;
  }
  startToolbarDrag(event);
});
window.addEventListener(
  "pointermove",
  (event) => {
    if (moveToolbarDrag(event)) event.stopPropagation();
  },
  true,
);
window.addEventListener(
  "pointerup",
  (event) => endToolbarDrag(event),
  true,
);
window.addEventListener(
  "pointercancel",
  (event) => endToolbarDrag(event),
  true,
);

function undo(): void {
  if (!engine) return;
  if (!engine.undo()) showTransient("没有可撤销的标注");
}

function redo(): void {
  if (!engine) return;
  if (!engine.redo()) showTransient("没有可重做的标注");
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

function normalizeAfterCapture(value: unknown): AfterCapture {
  // R8 removed the main-window editor: older "editor" values fall back to the
  // in-place annotation toolbar.
  if (value === "clipboard" || value === "pin") return value;
  return "inplace";
}

function reloadOverlayPreferences(): void {
  storedPreferences = loadPreferences();
  color = normalizeColor(storedPreferences.defaultColor) ?? DEFAULT_PREFERENCES.defaultColor;
  penWidth = clamp(Math.round(storedPreferences.defaultWidth), 1, 10);
  currentTool = isDrawTool(storedPreferences.defaultTool)
    ? storedPreferences.defaultTool : DEFAULT_PREFERENCES.defaultTool as ToolId;
  tooltipsEnabled = storedPreferences.tooltip;
  tooltipDelayMs = clamp(Math.round(storedPreferences.tooltipDelay), 0, 2000);
  enterCopyEnabled = storedPreferences.enterCopy;
  watermarkText = storedPreferences.watermarkText || "Pinstill";
  watermarkOpacity = clamp(storedPreferences.watermarkOpacity, 10, 90);
  watermark = loadWatermarkSettings();
  textStyle = loadTextStyle();
  document.documentElement.style.setProperty("--tooltip-delay", `${tooltipDelayMs}ms`);
  toolbar.classList.toggle("no-tooltips", !tooltipsEnabled);
  updateWidthValue();
  setColor(color);
  selectTool(currentTool);
  applyTextStyleUI();
  updateWatermarkUI();
}

function loadSettings(): Promise<void> {
  const sequence = ++settingsSequence;
  return settings()
    .then((current) => {
      if (sequence === settingsSequence) afterCapture = normalizeAfterCapture(current.afterCapture);
    })
    .catch(() => {
      if (sequence === settingsSequence) afterCapture = "inplace";
    });
}

function mappedAfterCapture(): { go: FinishAnnotatedAction; ui: ToolbarAction } | null {
  if (afterCapture === "clipboard") return { go: "copy", ui: "copy" };
  if (afterCapture === "pin") return { go: "pin", ui: "pin" };
  return null;
}

// ---------------------------------------------------------------------------
// Prepare → annotate
// ---------------------------------------------------------------------------

function currentAttempt(attempt: number): boolean {
  return attempt === captureAttempt;
}

function releaseCaptureImage(): void {
  if (captureImage && "close" in captureImage) captureImage.close();
  captureImage = null;
}

async function fetchCaptureBlob(url: string): Promise<Blob> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`图片请求失败（HTTP ${response.status}）`);
  const blob = await response.blob();
  if (blob.size > MAX_ENCODED_BYTES) throw new Error("图片文件超过上限（64 MiB）");
  return blob;
}

function editSelectedText(): void {
  const shape = selectedAnnotation();
  if (shape?.kind !== "text" && shape?.kind !== "tag") return;
  openInlineText({ x: shape.x + (shape.dx ?? 0), y: shape.y + (shape.dy ?? 0) }, shape);
}

function openInlineText(point: Point, shape?: Readonly<Shape>): void {
  const activeEngine = engine;
  if (phase !== "annotating" || !activeEngine || inlineEditor) return;
  const tool = shape?.kind ?? currentTool;
  if (tool !== "text" && tool !== "tag") return;
  const shapeColor = shape?.color ?? color;
  const shapeWidth = shape?.width ?? penWidth;
  const isText = tool === "text";
  const style = activeTextStyle();
  inlineEditor = new InlineTextEditor({
    canvas,
    point,
    color: shapeColor,
    displayFontSize: isText ? style.size : 20,
    bold: isText && style.bold,
    italic: isText && style.italic,
    fontFamily: isText ? style.family : DEFAULT_TEXT_FONT,
    value: shape?.text,
    placeholder: tool === "tag" ? "输入标签" : "输入文字",
    singleLine: tool === "tag",
    onSubmit: (text) => {
      inlineEditor = null;
      if (engine !== activeEngine) return;
      if (shape) {
        if (selectedAnnotation() === shape) activeEngine.editSelectedShape({ text });
        return;
      }
      activeEngine.addShape({
        kind: tool,
        x: point.x,
        y: point.y,
        x2: point.x,
        y2: point.y,
        color: shapeColor,
        width: shapeWidth,
        text,
        ...(isText
          ? {
              fontSize: style.size,
              bold: style.bold,
              italic: style.italic,
              fontFamily: style.family,
            }
          : {}),
      });
    },
    onCancel: () => {
      inlineEditor = null;
    },
  });
}

async function beginAnnotation(payload: OverlayCapture, attempt: number): Promise<void> {
  await settingsReady;
  if (!currentAttempt(attempt)) return;
  const rect = payload.rect ?? committedRect ?? selectionRect();
  const width = Math.round(payload.width ?? rect.width);
  const height = Math.round(payload.height ?? rect.height);
  checkImageDimensions(width, height);
  const blob = await fetchCaptureBlob(payload.url);
  if (!currentAttempt(attempt)) return;
  const autoAction = mappedAfterCapture();

  const image = await decodeImageBlob(blob);
  if (!currentAttempt(attempt)) {
    if ("close" in image) image.close();
    return;
  }
  let installed = false;
  try {
    checkImageDimensions("naturalWidth" in image ? image.naturalWidth : image.width ?? width,
      "naturalHeight" in image ? image.naturalHeight : image.height ?? height);
    committedRect = rect;
    selection.classList.remove("capturing");
    const displayed = editingRect(rect);
    selection.classList.toggle("fitted", !sameRect(rect, displayed));
    positionSelection(displayed);
    // Clear the old width first: mixing a new width with the previous height
    // could otherwise temporarily allocate more pixels than either image.
    canvas.width = 0;
    canvas.height = height;
    canvas.width = width;
    canvas.hidden = false;
    prepareState.hidden = true;
    dim.hidden = true;
    updateSizeBadge(rect, false, undefined, displayed);

    const ctx = canvas.getContext("2d");
    if (!ctx) throw new Error("画布不可用");
    engine = new AnnotationEngine({
      canvas,
      getStyle: () => ({ color, width: penWidth, entry }),
      getWatermarkOpacity: () => watermarkOpacity,
      onDraw: () => updateToolbarState(),
    });
    engine.setBaseRenderer(() => {
      ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
    });
    const displayedWidth = canvas.getBoundingClientRect().width || displayed.width;
    engine.setScale(canvas.width / Math.max(1, displayedWidth));
    engine.setTool(currentTool);
    engine.reset();
    engine.setWatermark(watermarkLayer());
    engine.redraw();

    phase = "annotating";
    hint.hidden = true;
    crosshairV.hidden = true;
    crosshairH.hidden = true;
    showToolbar();
    updateToolbarState();
    // Keep the previous source usable until drawing the replacement succeeds;
    // resize rollback may need it to redraw the original annotations.
    releaseCaptureImage();
    captureImage = image;
    installed = true;
  } finally {
    if (!installed && "close" in image) image.close();
  }
  if (autoAction) {
    // Keep an editable surface ready if automatic copy/pin fails.
    const rawBase64 = await blobToBase64(blob);
    if (!currentAttempt(attempt)) return;
    await finishWith(autoAction.go, rawBase64, autoAction.ui);
  }
}

/** Resolves with the promise result, or rejects after `ms` so a stuck native
 * capture can never leave the overlay frozen behind a spinner. */
function withTimeout<T>(promise: Promise<T>, ms: number, label: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error(label)), ms);
    promise.then(
      (value) => {
        window.clearTimeout(timer);
        resolve(value);
      },
      (error: unknown) => {
        window.clearTimeout(timer);
        reject(error);
      },
    );
  });
}

async function prepareSelectionFor(
  rect: Rect,
  fast = false,
  onFailure?: (error: unknown) => void,
): Promise<void> {
  const attempt = ++captureAttempt;
  phase = "preparing";
  precision.reset();
  toolbarMoved = false;
  lastCanvasClick = null;
  selectionResize = null;
  selection.classList.remove("resizing");
  // Go captures straight through the overlay window: hide the border, corner
  // marks, size badge, crosshair and hint so the captured image contains none
  // of our UI (the selection interior is already transparent).
  hideToolbar();
  selection.classList.add("capturing");
  // No "capturing" spinner here: the wait is ~250ms at most and the spinner
  // looked like a hang while adjusting the region. A watchdog below recovers
  // if the native capture ever stalls.
  prepareState.hidden = true;
  hint.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  sizeBadge.hidden = true;
  demoToast.hidden = true;
  try {
    if (fast) {
      // Let the browser paint the chrome-free frame before Go grabs the screen;
      // the timer fallback means rAF throttling can never stall the capture.
      await new Promise<void>((resolve) => {
        let done = false;
        const finish = () => {
          if (done) return;
          done = true;
          resolve();
        };
        requestAnimationFrame(() => requestAnimationFrame(finish));
        window.setTimeout(finish, 120);
      });
      if (!currentAttempt(attempt)) return;
    }
    const preparation = (async () => {
      const payload = fast
        ? await prepareSelectionFast(rect)
        : await withTimeout(prepareSelection(rect), 10000, "截取超时");
      if (!currentAttempt(attempt)) return;
      if (!payload?.url) throw new Error("截图数据无效");
      await beginAnnotation(payload, attempt);
    })();
    // A stalled image fetch/decode must restore a resized selection too.
    if (fast) await withTimeout(preparation, 4000, "重新截取超时");
    else await preparation;
  } catch (error) {
    if (!currentAttempt(attempt)) return;
    // Invalidate pending fetch/decode work before restoring the old surface.
    ++captureAttempt;
    if (onFailure) {
      onFailure(error);
      return;
    }
    const message = errorMessage(error);
    // The request error currently carries only text; capture:failed has no
    // request identity and cannot safely supply its permission flag here.
    showCaptureFailure(message, /屏幕录制权限|screen recording permission/i.test(message));
  }
}

// ---------------------------------------------------------------------------
// In-overlay failure guidance (no main window involved)
// ---------------------------------------------------------------------------

function hideCaptureError(): void {
  captureError.hidden = true;
}

function showCaptureFailure(rawMessage: string, permission: boolean): void {
  reset();
  phase = "finishing";
  hideToolbar();
  selection.classList.remove("capturing");
  positionMasks(null);
  selection.hidden = true;
  dim.hidden = false;
  sizeBadge.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = true;
  demoToast.hidden = true;
  captureError.hidden = false;
  captureErrorTitle.textContent = permission ? "需要屏幕录制权限" : "截图失败";
  const message = rawMessage || (permission ? "无法读取屏幕内容。" : "截图失败，请重试。");
  captureErrorText.textContent = permission
    ? `${message}\n请在「系统设置 › 隐私与安全性 › 屏幕录制」中允许 Pinstill，然后重试。`
    : message;
  captureErrorSettings.hidden = !permission;
}

function dismissCaptureFailure(): void {
  hideCaptureError();
  detectedWindows = [];
  reset();
  // The 取消 button ends the capture session (like Esc elsewhere); Go hides the
  // overlay. The page never expects the main window from a failure.
  if (!hasNative) return;
  void cancelRegion().catch(() => {});
}

async function retryCapture(): Promise<void> {
  hideCaptureError();
  reset();
  if (!hasNative) return;
  try {
    await beginRegion();
  } catch (error) {
    showTransient(`无法重新开始截图：${errorMessage(error)}`);
  }
}

// ---------------------------------------------------------------------------
// Finish
// ---------------------------------------------------------------------------

function actionButton(action: ToolbarAction): HTMLButtonElement | null {
  return toolbar.querySelector<HTMLButtonElement>(`button[data-action="${action}"]`);
}

function demoFinish(action: ToolbarAction): void {
  const button = actionButton(action);
  setButtonPending(button, false);
  if (action !== "cancel") flashButtonSuccess(button);
  showTransient(`浏览器预览 · ${DEMO_MESSAGES[action]}`, 3200);
  setToolbarPending(false);
  if (demoResetTimer) window.clearTimeout(demoResetTimer);
  demoResetTimer = window.setTimeout(() => reset(), 1900);
}

async function finishWith(
  action: FinishAnnotatedAction,
  base64: string,
  uiAction: ToolbarAction = action,
): Promise<void> {
  if (phase === "finishing") return;
  const attempt = captureAttempt;
  phase = "finishing";
  palettePanel.close();
  const button = actionButton(uiAction);
  setButtonPending(button, true);
  setToolbarPending(true, PENDING_LABELS[uiAction]);
  if (!hasNative) {
    demoFinish(uiAction);
    return;
  }
  try {
    const encodedLimit = Math.ceil(MAX_ENCODED_BYTES / 3) * 4;
    if (base64.length > encodedLimit || (base64.length / 4 * 3 -
      (base64.endsWith("==") ? 2 : base64.endsWith("=") ? 1 : 0)) > MAX_ENCODED_BYTES) {
      throw new Error("图片文件超过上限（64 MiB）");
    }
    const result = await finishAnnotated(action, base64);
    if (!currentAttempt(attempt)) return;
    if (result.completed || (action === "cancel" && result.cancelled)) {
      detectedWindows = [];
      reset();
      return;
    }
    if (!result.cancelled) throw new Error("操作未完成，请重试");
    setButtonPending(button, false);
    phase = "annotating";
    showToolbar();
    setToolbarStatus("已取消保存，可继续标注");
  } catch (error) {
    if (!currentAttempt(attempt)) return;
    const message = errorMessage(error);
    setButtonPending(button, false);
    if (!engine) {
      reset();
      showTransient(`操作失败：${message} · 请重新拖动选择`);
      return;
    }
    phase = "annotating";
    if (toolbar.hidden) showToolbar();
    setToolbarPending(false);
    setToolbarStatus(`操作失败：${message}`, "error");
  }
}

async function runAction(action: ToolbarAction): Promise<void> {
  if (action !== "cancel" && keyboardAdjustment && !await settleKeyboardAdjustment()) return;
  if (phase !== "annotating") return;
  if (action === "cancel") {
    await finishWith("cancel", "", "cancel");
    return;
  }
  if (!engine) return;
  try {
    inlineEditor?.submit();
    if (drawingPointer !== null) {
      engine.endDraw();
      drawingPointer = null;
    }
    await finishWith(action, engine.toPNGBase64(), action);
  } catch (error) {
    setToolbarStatus(`导出失败：${errorMessage(error)}`, "error");
  }
}

// Showcase/test hook: render a generated image as the captured base, so the
// README screenshots can show the annotation toolbar without the user's
// desktop. It is only called by the app's own screenshot selftest.
type DemoCaptureHook = (dataUrl: string) => Promise<void>;
const hookHost = globalThis as typeof globalThis & { __pinstillDemoCapture?: DemoCaptureHook };
hookHost.__pinstillDemoCapture = async (dataUrl: string): Promise<void> => {
  const image = new Image();
  image.src = dataUrl;
  await image.decode();
  if (!engine) return;
  engine.setBaseRenderer(() => {
    const ctx = canvas.getContext("2d");
    if (ctx) ctx.drawImage(image, 0, 0, canvas.width, canvas.height);
  });
  engine.redraw();
};

// ---------------------------------------------------------------------------
// Selection flow
// ---------------------------------------------------------------------------

function reset(): void {
  ++captureAttempt;
  precision.reset();
  if (keyboardAdjustment) window.clearTimeout(keyboardAdjustment.timer);
  keyboardAdjustment = null;
  releaseCaptureImage();
  phase = "idle";
  watermarkEnabled = false;
  if (inlineEditor) {
    const editor = inlineEditor;
    inlineEditor = null;
    editor.cancel();
  }
  hideCaptureError();
  positionMasks(null);
  pressWindow = null;
  dragMoved = false;
  passiveSelection = false;
  peerPreview = null;
  selectionResize = null;
  selection.classList.remove("capturing", "window-preview", "resizing", "fitted");
  if (transientTimer) window.clearTimeout(transientTimer);
  if (demoResetTimer) window.clearTimeout(demoResetTimer);
  transientTimer = undefined;
  demoResetTimer = undefined;
  engine = null;
  drawingPointer = null;
  committedRect = null;
  startX = 0;
  startY = 0;
  currentX = 0;
  currentY = 0;
  positionMasks(null);
  dim.hidden = false;
  selection.hidden = true;
  canvas.hidden = true;
  canvas.width = 0;
  canvas.height = 0;
  prepareState.hidden = true;
  sizeBadge.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.innerHTML = hintInitialHTML;
  hint.hidden = false;
  demoToast.hidden = true;
  hideToolbar();
  document.body.style.cursor = "crosshair";
}

function beginSelection(x: number, y: number, snapped: OverlayWindow | null = null): void {
  phase = "selecting";
  selection.classList.remove("capturing");
  startX = x;
  startY = y;
  currentX = x;
  currentY = y;
  endpointOffsetX = 0;
  endpointOffsetY = 0;
  pressWindow = snapped;
  dragMoved = false;
  hint.hidden = true;
  demoToast.hidden = true;
  canvas.hidden = true;
  prepareState.hidden = true;
  hideToolbar();
  moveCrosshair(x, y);
  if (snapped) {
    showWindowPreview(snapped);
  } else {
    selection.classList.remove("window-preview");
    renderSelection();
  }
}

function cancelRegionFlow(): void {
  if (phase === "finishing") return;
  detectedWindows = [];
  reset();
  const attempt = captureAttempt;
  phase = "finishing";
  hideToolbar();
  positionMasks(null);
  selection.hidden = true;
  sizeBadge.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = true;
  demoToast.hidden = true;
  if (!hasNative) {
    reset();
    return;
  }
  void cancelRegion().catch(() => { if (currentAttempt(attempt)) reset(); });
}

// ---------------------------------------------------------------------------
// Pointer & keyboard
// ---------------------------------------------------------------------------

function startSelectionResize(event: PointerEvent, direction: ResizeDirection): void {
  if (phase !== "annotating" || !committedRect || selectionResize || selection.classList.contains("fitted")) return;
  if (inlineEditor) {
    inlineEditor.cancel();
    inlineEditor = null;
  }
  selectionResize = {
    direction,
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    rect: { ...committedRect },
  };
  selection.classList.add("resizing");
  if (event.target instanceof HTMLElement) {
    try {
      event.target.setPointerCapture(event.pointerId);
    } catch {}
  }
  event.preventDefault();
  event.stopPropagation();
}

function resizeRectFor(event: PointerEvent): Rect {
  const drag = selectionResize;
  if (!drag) return committedRect ?? { x: 0, y: 0, width: 0, height: 0 };
  const dx = event.clientX - drag.startX;
  const dy = event.clientY - drag.startY;
  const base = drag.rect;
  let x1 = base.x;
  let y1 = base.y;
  let x2 = base.x + base.width;
  let y2 = base.y + base.height;
  const minWidth = Math.min(base.width, MIN_RESIZE);
  const minHeight = Math.min(base.height, MIN_RESIZE);
  if (drag.direction.includes("w")) {
    x1 = clamp(base.x + dx, 0, x2 - minWidth);
  }
  if (drag.direction.includes("e")) {
    x2 = clamp(base.x + base.width + dx, x1 + minWidth, viewportWidth());
  }
  if (drag.direction.includes("n")) {
    y1 = clamp(base.y + dy, 0, y2 - minHeight);
  }
  if (drag.direction.includes("s")) {
    y2 = clamp(base.y + base.height + dy, y1 + minHeight, viewportHeight());
  }
  return { x: x1, y: y1, width: x2 - x1, height: y2 - y1 };
}

/** Rebuilds the drawing engine around the current canvas content (used after a
 * local crop, without a screen grab). */
function installEngineForCanvas(base: HTMLCanvasElement, displayWidth: number): void {
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  engine = new AnnotationEngine({
    canvas,
    getStyle: () => ({ color, width: penWidth, entry }),
    getWatermarkOpacity: () => watermarkOpacity,
    onDraw: () => updateToolbarState(),
  });
  engine.setBaseRenderer(() => {
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.drawImage(base, 0, 0, canvas.width, canvas.height);
  });
  engine.setScale(canvas.width / Math.max(1, displayWidth));
  engine.setTool(currentTool);
  engine.reset();
  engine.setWatermark(watermarkLayer());
  engine.redraw();
}

async function recaptureSelection(rect: Rect, previous: Rect): Promise<void> {
  if (phase !== "annotating") return;
  inlineEditor?.cancel();
  inlineEditor = null;
  const hadAnnotations = Boolean(engine?.getShapes().length);
  // Shrinking is instant: the new region lies inside the capture we already
  // have, so crop it locally instead of grabbing the screen again.
  if (
    previous && captureImage &&
    rect.x >= previous.x - 0.5 &&
    rect.y >= previous.y - 0.5 &&
    rect.x + rect.width <= previous.x + previous.width + 0.5 &&
    rect.y + rect.height <= previous.y + previous.height + 0.5 &&
    canvas.width > 0 &&
    canvas.height > 0
  ) {
    const scaleX = canvas.width / previous.width;
    const scaleY = canvas.height / previous.height;
    const sx = Math.max(0, Math.round((rect.x - previous.x) * scaleX));
    const sy = Math.max(0, Math.round((rect.y - previous.y) * scaleY));
    const sw = Math.max(1, Math.min(canvas.width - sx, Math.round(rect.width * scaleX)));
    const sh = Math.max(1, Math.min(canvas.height - sy, Math.round(rect.height * scaleY)));
    const cropped = document.createElement("canvas");
    cropped.width = sw;
    cropped.height = sh;
    const cropCtx = cropped.getContext("2d");
    if (!cropCtx) return;
    // Crop the original pixels. Cropping the composited canvas would bake in
    // the annotations and watermark before the new engine draws them again.
    cropCtx.drawImage(captureImage, sx, sy, sw, sh, 0, 0, sw, sh);
    releaseCaptureImage();
    captureImage = cropped;
    canvas.width = sw;
    canvas.height = sh;
    canvas.hidden = false;
    committedRect = rect;
    selection.classList.remove("capturing");
    positionSelection(rect);
    installEngineForCanvas(cropped, rect.width);
    updateToolbarState();
    if (hadAnnotations) {
      showTransient("选区已调整 · 原有标注已重置", 2200);
    }
    return;
  }
  const previousEngine = engine;
  const previousWidth = canvas.width;
  const previousHeight = canvas.height;
  committedRect = rect;
  positionSelection(rect);
  const preparation = prepareSelectionFor(rect, true, (error) => {
    // Called only for the current attempt. The old source stays owned by this
    // session until beginAnnotation successfully installs its replacement.
    committedRect = previous;
    engine = previousEngine;
    canvas.width = 0;
    canvas.height = previousHeight;
    canvas.width = previousWidth;
    canvas.hidden = false;
    phase = "annotating";
    prepareState.hidden = true;
    dim.hidden = true;
    selection.classList.remove("capturing");
    positionSelection(previous);
    engine?.redraw();
    showToolbar();
    showTransient(`重新截取失败 · 已恢复原选区：${errorMessage(error)}`);
  });
  const attempt = captureAttempt;
  await preparation;
  if (currentAttempt(attempt) && hadAnnotations && phase === "annotating") {
    showTransient("选区已调整 · 已重新截取（原有标注已重置）", 2200);
  }
}

function canvasPoint(event: PointerEvent, allowOutside = false): Point | null {
  const rect = canvas.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  const x = ((event.clientX - rect.left) * canvas.width) / rect.width;
  const y = ((event.clientY - rect.top) * canvas.height) / rect.height;
  if (!allowOutside && (x < -4 || y < -4 || x > canvas.width + 4 || y > canvas.height + 4)) return null;
  return { x: clamp(x, 0, canvas.width), y: clamp(y, 0, canvas.height) };
}

function applyKeyboardAdjustment(): Promise<void> | undefined {
  if (!keyboardAdjustment) return;
  const adjustment = keyboardAdjustment;
  keyboardAdjustment = null;
  window.clearTimeout(adjustment.timer);
  return recaptureSelection(adjustment.rect, adjustment.original);
}

async function settleKeyboardAdjustment(): Promise<boolean> {
  const pending = applyKeyboardAdjustment();
  const attempt = captureAttempt;
  await pending;
  return currentAttempt(attempt);
}

function handlePrecisionKey(event: KeyboardEvent): boolean {
  if (event.metaKey || event.ctrlKey || selectionResize || drawingPointer !== null ||
    (event.target instanceof Element && event.target.closest("button, input, textarea, select, [contenteditable]"))) {
    return false;
  }
  const key = event.key.toLowerCase();
  const horizontal = key === "arrowleft" ? -1 : key === "arrowright" ? 1 : 0;
  const vertical = key === "arrowup" ? -1 : key === "arrowdown" ? 1 : 0;
  if (phase === "idle" || phase === "selecting") {
    if (!event.altKey && !event.shiftKey && ["z", "c", "m"].includes(key)) {
      if (!event.repeat) {
        if (key === "z") precision.toggleLoupe();
        else if (key === "m") precision.toggleRuler();
        else void precision.copyColor();
      }
      return true;
    }
    if (phase !== "selecting" || (!horizontal && !vertical) || event.altKey) return false;
    const step = event.shiftKey ? 10 : 1;
    const desktop = desktopBounds();
    const nextX = clamp(currentX + horizontal * step, desktop.x, desktop.x + desktop.width);
    const nextY = clamp(currentY + vertical * step, desktop.y, desktop.y + desktop.height);
    endpointOffsetX += nextX - currentX;
    endpointOffsetY += nextY - currentY;
    currentX = nextX;
    currentY = nextY;
    dragMoved = true;
    pressWindow = null;
    selection.classList.remove("window-preview");
    moveCrosshair(currentX, currentY);
    renderSelection();
    return true;
  }
  if (phase !== "annotating" || !committedRect || selection.classList.contains("fitted") || !event.altKey || (!horizontal && !vertical)) return false;

  const original = keyboardAdjustment?.original ?? committedRect;
  const previous = keyboardAdjustment?.rect ?? original;
  const rect = { ...previous };
  if (event.shiftKey) {
    if (horizontal) rect.width = clamp(rect.width + horizontal, Math.min(MIN_RESIZE, original.width), viewportWidth() - rect.x);
    if (vertical) rect.height = clamp(rect.height + vertical, Math.min(MIN_RESIZE, original.height), viewportHeight() - rect.y);
  } else {
    if (horizontal) rect.x = clamp(rect.x + horizontal, 0, viewportWidth() - rect.width);
    if (vertical) rect.y = clamp(rect.y + vertical, 0, viewportHeight() - rect.height);
  }
  if (sameRect(previous, rect)) return true;
  if (keyboardAdjustment) window.clearTimeout(keyboardAdjustment.timer);
  if (sameRect(rect, original)) keyboardAdjustment = null;
  else keyboardAdjustment = { original, rect, timer: window.setTimeout(applyKeyboardAdjustment, 140) };
  canvas.hidden = keyboardAdjustment !== null;
  positionSelection(rect);
  updateSizeBadge(rect);
  return true;
}

window.addEventListener("pointerdown", (event) => {
  if (isInsideTextEditor(event.target)) return;
  if (isInsidePalette(event.target)) return;
  if (keyboardAdjustment && !isInsideToolbar(event.target)) {
    void applyKeyboardAdjustment();
    return;
  }
  if (phase === "annotating" && event.button === 0 && event.target instanceof Element) {
    const handle = event.target.closest<HTMLElement>("[data-resize]");
    const direction = RESIZE_DIRECTIONS.find((value) => value === handle?.dataset.resize);
    if (direction) {
      startSelectionResize(event, direction);
      return;
    }
  }
  if (phase === "annotating") {
    if (isInsideToolbar(event.target)) return;
    if (event.button !== 0 || !engine || drawingPointer !== null) return;
    const point = canvasPoint(event);
    if (!point) return;
    const now = performance.now();
    const previousClick = lastCanvasClick;
    const isDoubleClick =
      previousClick !== null &&
      now - previousClick.time <= 350 &&
      Math.hypot(event.clientX - previousClick.x, event.clientY - previousClick.y) <= 8;
    lastCanvasClick = { time: now, x: event.clientX, y: event.clientY };
    if (isDoubleClick && currentTool !== "select") {
      // A double-click on the image means "done": commit any pending inline
      // text, then copy the annotated capture (same path as the 复制 button).
      event.preventDefault();
      lastCanvasClick = null;
      if (inlineEditor) inlineEditor.submit();
      void runAction("copy");
      return;
    }
    if (inlineEditor) {
      // Clicking the canvas while typing commits the current text first.
      inlineEditor.submit();
      return;
    }
    if (currentTool === "text" || currentTool === "tag") {
      event.preventDefault();
      openInlineText(point);
      return;
    }
    event.preventDefault();
    if (!engine.beginDraw(point)) return;
    if (currentTool === "select" && isDoubleClick) {
      lastCanvasClick = null;
      engine.endDraw();
      editSelectedText();
      return;
    }
    drawingPointer = event.pointerId;
    try {
      canvas.setPointerCapture(event.pointerId);
    } catch {
      // Synthetic events (E2E) have no capturable pointer id; window listeners
      // still receive the moves.
    }
    return;
  }
  if (phase !== "idle" && phase !== "selecting") return;
  if (passiveSelection) return;
  if (isInsideToolbar(event.target)) return;
  if (event.button !== 0) {
    event.preventDefault();
    cancelRegionFlow();
    return;
  }
  event.preventDefault();
  try {
    document.documentElement.setPointerCapture(event.pointerId);
  } catch {
    // Window listeners still see the drag.
  }
  const x = clamp(event.clientX, 0, viewportWidth());
  const y = clamp(event.clientY, 0, viewportHeight());
  const snapped = findWindowAt(x, y);
  beginSelection(x, y, snapped);
});

window.addEventListener("pointermove", (event) => {
  if (selectionResize) {
    if (event.pointerId !== selectionResize.pointerId) return;
    event.preventDefault();
    const rect = resizeRectFor(event);
    canvas.hidden = !sameRect(rect, selectionResize.rect);
    committedRect = rect;
    positionSelection(rect);
    updateSizeBadge(rect);
    return;
  }
  if (phase === "idle") {
    if (passiveSelection) return;
    const x = clamp(event.clientX, 0, viewportWidth());
    const y = clamp(event.clientY, 0, viewportHeight());
    moveCrosshair(x, y);
    updateHoveredWindow(x, y);
    return;
  }
  if (phase === "selecting") {
    const desktop = desktopBounds();
    currentX = clamp(event.clientX + endpointOffsetX, desktop.x, desktop.x + desktop.width);
    currentY = clamp(event.clientY + endpointOffsetY, desktop.y, desktop.y + desktop.height);
    moveCrosshair(currentX, currentY);
    if (
      !dragMoved &&
      (Math.abs(currentX - startX) >= MIN_SELECTION ||
        Math.abs(currentY - startY) >= MIN_SELECTION)
    ) {
      dragMoved = true;
      pressWindow = null;
      selection.classList.remove("window-preview");
    }
    if (dragMoved || !pressWindow) {
      renderSelection();
    }
    return;
  }
  if (phase === "annotating" && drawingPointer !== null && event.pointerId === drawingPointer && engine) {
    if (lastCanvasClick && Math.hypot(event.clientX - lastCanvasClick.x, event.clientY - lastCanvasClick.y) > 3) {
      lastCanvasClick = null;
    }
    const point = canvasPoint(event, true);
    if (point) engine.moveDraw(point);
  }
});

document.documentElement.addEventListener("pointerenter", (event) => {
  if (phase === "idle" && !passiveSelection && displayInfo?.desktop && (event.buttons & 1) === 0) {
    queuePeerPreview(null);
  }
});

document.documentElement.addEventListener("pointerleave", () => {
  if (phase !== "idle" || passiveSelection) return;
  precision.leave();
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  showWindowPreview(null);
});

window.addEventListener("pointerup", (event) => {
  if (selectionResize) {
    if (event.pointerId !== selectionResize.pointerId) return;
    const original = selectionResize.rect;
    const rect = { ...(committedRect ?? original) };
    selectionResize = null;
    selection.classList.remove("resizing");
    if (sameRect(original, rect)) {
      committedRect = original;
      canvas.hidden = false;
      positionSelection(original);
      updateSizeBadge(original);
      return;
    }
    void recaptureSelection(rect, original);
    return;
  }
  if (phase === "selecting" && event.button === 0) {
    if (!dragMoved && pressWindow) {
      const finalRect = clampWindowRect(pressWindow);
      pressWindow = null;
      selection.classList.remove("window-preview");
      if (finalRect.width < MIN_SELECTION || finalRect.height < MIN_SELECTION) {
        reset();
        return;
      }
      committedRect = finalRect;
      positionSelection(finalRect);
      void prepareSelectionFor(finalRect);
      return;
    }
    const rect = selectionRect();
    if (rect.width < MIN_SELECTION || rect.height < MIN_SELECTION) {
      reset();
      queuePeerPreview(null);
      return;
    }
    const finalRect = roundedSelectionRect();
    committedRect = finalRect;
    positionSelection(finalRect);
    void prepareSelectionFor(finalRect);
    return;
  }
  if (phase === "annotating" && drawingPointer !== null && event.pointerId === drawingPointer && engine) {
    engine.endDraw();
    drawingPointer = null;
  }
});

window.addEventListener("pointercancel", (event) => {
  if (phase === "selecting") {
    reset();
    queuePeerPreview(null);
    return;
  }
  if (drawingPointer !== null && event.pointerId === drawingPointer) {
    engine?.cancelDraw();
    lastCanvasClick = null;
    drawingPointer = null;
    return;
  }
  if (selectionResize?.pointerId !== event.pointerId) return;
  const original = selectionResize.rect;
  selectionResize = null;
  selection.classList.remove("resizing");
  committedRect = original;
  canvas.hidden = false;
  positionSelection(original);
  updateSizeBadge(original);
});

window.addEventListener("contextmenu", (event) => {
  event.preventDefault();
  if (phase === "idle" || phase === "selecting" || phase === "preparing") cancelRegionFlow();
});

window.addEventListener("keydown", (event) => {
  if (!captureError.hidden) {
    if (event.key === "Escape") {
      event.preventDefault();
      dismissCaptureFailure();
    }
    return;
  }
  if (inlineEditor) return; // the inline textarea handles Enter / Esc itself
  if (handlePrecisionKey(event)) {
    event.preventDefault();
    return;
  }
  if (event.key === "Escape") {
    event.preventDefault();
    if (phase === "annotating") {
      void finishWith("cancel", "");
      return;
    }
    if (phase === "idle" || phase === "selecting" || phase === "preparing") {
      cancelRegionFlow();
      return;
    }
    return;
  }
  if (phase === "annotating" && !isInsidePalette(event.target) &&
    !(event.target instanceof Element && event.target.closest("input, select, textarea, [contenteditable]"))) {
    const key = event.key.toLowerCase();
    const modifies = event.metaKey || event.ctrlKey;
    if (modifies && !event.altKey && (key === "z" || (event.ctrlKey && key === "y"))) {
      event.preventDefault();
      if (event.shiftKey || key === "y") redo();
      else undo();
      return;
    }
    if (!modifies && !event.altKey && drawingPointer === null) {
      if (key === "v") {
        event.preventDefault();
        selectTool("select");
        return;
      }
      if ((key === "backspace" || key === "delete") && engine?.deleteSelectedShape()) {
        event.preventDefault();
        return;
      }
    }
  }
  if (event.key === "Enter" && enterCopyEnabled && phase === "annotating") {
    if (palettePanel.isOpen() && palettePanel.contains(event.target)) return;
    event.preventDefault();
    void runAction("copy");
  }
});

// ---------------------------------------------------------------------------
// Events / init
// ---------------------------------------------------------------------------

// PrepareSelection returns the same payload it broadcasts. Only consume the
// request result: overlay:capture and capture:failed lack an attempt identity,
// so late events from cancelled/timed-out requests cannot be matched safely.

captureErrorSettings.addEventListener("click", () => {
  void openPermissionSettings().catch(() => {});
});
captureErrorRetry.addEventListener("click", () => void retryCapture());
captureErrorCancel.addEventListener("click", () => dismissCaptureFailure());
function notifyOverlayReady(session: number | undefined, attempt: number): void {
  if (typeof session !== "number" || !Number.isFinite(session) || session <= 0) return;
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      if (!currentAttempt(attempt)) return;
      void overlayReady(session).catch(() => {});
    });
  });
}

onOverlayShown((payload) => {
  overlayShownHandled = true;
  // PrepareSelection re-shows the overlay without overlay:shown; if this is a
  // new BeginRegion session, reset everything.
  reset();
  reloadOverlayPreferences();
  applyOverlayInfo(payload);
  settingsReady = loadSettings();
  notifyOverlayReady(payload?.session, captureAttempt);
});

onOverlayPreview(({ session, rect }) => {
  if (session !== displayInfo?.session || phase !== "idle") return;
  passiveSelection = Boolean(rect);
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = passiveSelection;
  if (rect) {
    sizeBadge.hidden = true;
    selection.classList.remove("window-preview");
    positionSelection(rect);
  } else {
    showWindowPreview(null);
  }
});

buildToolbar();
updateWidthValue();
setColor(color);
reset();
settingsReady = loadSettings();
void overlayInfo()
  .then((info) => {
    if (!overlayShownHandled && phase === "idle") {
      applyOverlayInfo(info);
      notifyOverlayReady(info?.session, captureAttempt);
    }
  })
  .catch(() => {
    displayInfo = null;
  });
