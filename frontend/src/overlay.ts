// The region-selection overlay, Round 3: after the drag the selection becomes a
// real in-place annotation surface.
//
// Flow (plan R3.1): drag a selection → `Capture.PrepareSelection(rect)` → Go
// hides the overlay, captures the region, shows it again and emits
// `overlay:capture` → the page renders the captured PNG into
// `#overlayCanvas` inside the selection and shows the COMPLETE annotation
// toolbar (tools, color swatches, width/font slider, text entry, undo,
// 复制/贴图/保存/取消). Actions send the composited PNG through
// `Capture.FinishAnnotated(action, base64)`; Go performs them.
//
// When `afterCapture` is not "inplace" the toolbar is skipped: the raw captured
// PNG bytes go straight to `FinishAnnotated` with the mapped action. In a plain
// browser (no MyGo runtime) the flow is simulated with the design's demo image
// and toasts, so the static preview stays demoable without console errors.

import { AnnotationEngine, InlineTextEditor } from "./annotate";
import {
  beginRegion,
  cancelRegion,
  finishAnnotated,
  hasNative,
  onCaptureFailed,
  onOverlayCapture,
  onOverlayShown,
  openPermissionSettings,
  overlayInfo,
  prepareSelection,
  settings,
  startScroll,
  type FinishAnnotatedAction,
  type OverlayCapture,
  type OverlayInfo,
  type Rect,
} from "./bridge";
import {
  loadRecentColors,
  normalizeColor,
  PRESET_COLORS,
  rememberColor,
} from "./colors";
import { ColorPalettePanel, createPaletteToggleButton } from "./palette";
import { DRAW_TOOLS, TOOLS, TOOL_ICONS, type ToolId } from "./tools";
import { blobToBase64, decodeImageBlob, flashButtonSuccess, setButtonPending } from "./util";

const MIN_SELECTION = 3;
const TOOLBAR_GAP = 10;
const EDGE_MARGIN = 8;

type Phase = "idle" | "selecting" | "preparing" | "annotating" | "finishing";
type AfterCapture = "inplace" | "clipboard" | "pin";

/** Toolbar actions as exposed to the page/E2E (data-action). */
type ToolbarAction = "copy" | "pin" | "save" | "cancel";

const PENDING_LABELS: Record<ToolbarAction, string> = {
  copy: "正在复制…",
  pin: "正在固定到桌面…",
  save: "正在保存…",
  cancel: "正在取消…",
};

const DEMO_MESSAGES: Record<ToolbarAction, string> = {
  copy: "复制：桌面应用中会把合成图写入系统剪贴板",
  pin: "贴图：桌面应用中会创建置顶的桌面贴图窗口",
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
  copy: '<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
  pin: '<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
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
const selection = el("selection");
const canvas = el<HTMLCanvasElement>("overlayCanvas");
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
const widthLabelText = el("overlayWidthLabelText");
const textInput = el<HTMLInputElement>("overlayText");
const toolbarStatus = el("overlayStatus");
const demoToast = el("demoToast");
const captureError = el("captureError");
const captureErrorTitle = el("captureErrorTitle");
const captureErrorText = el("captureErrorText");
const captureErrorSettings = el<HTMLButtonElement>("captureErrorSettings");
const captureErrorRetry = el<HTMLButtonElement>("captureErrorRetry");
const captureErrorCancel = el<HTMLButtonElement>("captureErrorCancel");
const hintInitialHTML = hint.innerHTML;

interface StoredPreferences {
  defaultColor?: unknown;
  defaultWidth?: unknown;
  watermarkText?: unknown;
  watermarkOpacity?: unknown;
}

function readStoredPreferences(): StoredPreferences {
  try {
    return JSON.parse(localStorage.getItem("pinstill.preferences") ?? "{}") as StoredPreferences;
  } catch {
    return {};
  }
}

const storedPreferences = readStoredPreferences();
let color =
  normalizeColor(storedPreferences.defaultColor) ?? PRESET_COLORS[0]!;
let penWidth =
  typeof storedPreferences.defaultWidth === "number"
    ? clamp(Math.round(storedPreferences.defaultWidth), 1, 10)
    : 2;
const watermarkText =
  typeof storedPreferences.watermarkText === "string" && storedPreferences.watermarkText
    ? storedPreferences.watermarkText
    : "Pinstill";
const watermarkOpacity =
  typeof storedPreferences.watermarkOpacity === "number"
    ? clamp(storedPreferences.watermarkOpacity, 10, 90)
    : 35;

let currentTool: ToolId = "rect";
let entry = "重点说明";

let displayInfo: OverlayInfo | null = null;
let phase: Phase = "idle";
let afterCapture: AfterCapture = "inplace";
let settingsReady: Promise<void> = Promise.resolve();

let startX = 0;
let startY = 0;
let currentX = 0;
let currentY = 0;
let committedRect: Rect | null = null;

let engine: AnnotationEngine | null = null;
let captureHandled = false;
let handledCaptureId = "";
let drawingPointer: number | null = null;

let transientTimer: number | undefined;
let demoResetTimer: number | undefined;
let recentColors: string[] = loadRecentColors();
let inlineEditor: InlineTextEditor | null = null;

// The shared in-app palette panel (R7.8). `setColor` is a hoisted function
// declaration, so it is available when a colour is picked later.
const palettePanel = new ColorPalettePanel({
  getColor: () => color,
  onColor: (next, options) => setColor(next, options.remember),
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
  const x = clamp(Math.round(rect.x), 0, viewportWidth());
  const y = clamp(Math.round(rect.y), 0, viewportHeight());
  return {
    x,
    y,
    width: Math.max(1, Math.round(Math.min(rect.width, viewportWidth() - x))),
    height: Math.max(1, Math.round(Math.min(rect.height, viewportHeight() - y))),
  };
}

function moveCrosshair(x: number, y: number): void {
  crosshairV.hidden = false;
  crosshairH.hidden = false;
  crosshairV.style.left = `${x}px`;
  crosshairH.style.top = `${y}px`;
}

function updateSizeBadge(rect: Rect, toolbarAbove = false): void {
  sizeBadge.textContent = `${Math.round(rect.width)} × ${Math.round(rect.height)}`;
  sizeBadge.hidden = false;
  const badgeWidth = sizeBadge.offsetWidth || 84;
  const badgeHeight = sizeBadge.offsetHeight || 24;
  let left = rect.x + rect.width - badgeWidth;
  let top = toolbarAbove ? rect.y + 8 : rect.y - badgeHeight - 8;
  if (top < EDGE_MARGIN) top = rect.y + 8;
  left = clamp(left, EDGE_MARGIN, Math.max(EDGE_MARGIN, viewportWidth() - badgeWidth - EDGE_MARGIN));
  top = clamp(top, EDGE_MARGIN, Math.max(EDGE_MARGIN, viewportHeight() - badgeHeight - EDGE_MARGIN));
  sizeBadge.style.left = `${left}px`;
  sizeBadge.style.top = `${top}px`;
}

function renderSelection(): void {
  const rect = selectionRect();
  selection.hidden = false;
  dim.hidden = true;
  selection.style.left = `${rect.x}px`;
  selection.style.top = `${rect.y}px`;
  selection.style.width = `${rect.width}px`;
  selection.style.height = `${rect.height}px`;
  updateSizeBadge(rect);
}

function positionSelection(rect: Rect): void {
  selection.hidden = false;
  dim.hidden = true;
  selection.style.left = `${rect.x}px`;
  selection.style.top = `${rect.y}px`;
  selection.style.width = `${rect.width}px`;
  selection.style.height = `${rect.height}px`;
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
  for (const tool of DRAW_TOOLS) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tool-btn";
    button.dataset.tool = tool;
    button.setAttribute("aria-label", toolLabel(tool));
    button.setAttribute("aria-pressed", "false");
    button.innerHTML = `${svgIcon(TOOL_ICONS[tool] ?? "square")}<span class="tip">${toolLabel(tool)}</span>`;
    toolsHost.append(button);
  }
  const undo = document.createElement("button");
  undo.type = "button";
  undo.className = "tool-btn";
  undo.dataset.tool = "undo";
  undo.setAttribute("aria-label", "撤销");
  undo.disabled = true;
  undo.innerHTML = `${svgIcon("undo-2")}<span class="tip">撤销</span>`;
  toolsHost.append(undo);

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
    penWidth = clamp(Number(widthInput.value) || 1, 1, 10);
    widthValue.textContent = String(penWidth);
  });
  textInput.addEventListener("input", () => {
    entry = textInput.value;
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
    else if (tool === "scroll") void startScrollFromToolbar();
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
  if (phase !== "annotating" || !committedRect) return;
  if (!hasNative) {
    setToolbarStatus("滚动截图 · 浏览器预览", "info");
    showTransient(
      "浏览器预览 · 桌面应用中会默认自动滚动、自动完成并弹出长图预览窗口",
      3400,
    );
    return;
  }
  setToolbarPending(true, "正在进入滚动截图…");
  try {
    // R9: scrolling capture starts in automatic mode; Go falls back to manual
    // (and reports autoDenied on the control bar) when Accessibility is missing.
    await startScroll(committedRect, true);
    phase = "finishing";
    setToolbarStatus("滚动截图进行中 · 结束后自动预览");
  } catch (error) {
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

function selectTool(tool: ToolId): void {
  currentTool = tool;
  engine?.setTool(tool);
  for (const button of toolbar.querySelectorAll<HTMLButtonElement>("button[data-tool]")) {
    if (button.dataset.tool === "undo") continue;
    const active = button.dataset.tool === tool;
    button.classList.toggle("active", active);
    button.setAttribute("aria-pressed", String(active));
  }
  // Text and tag now use the inline editor at the clicked position; only the
  // watermark keeps the toolbar text field.
  const showsTextInput = tool === "watermark";
  textInput.hidden = !showsTextInput;
  widthLabelText.textContent =
    tool === "text" || tool === "watermark" ? "字号" : "粗细";
  if (tool === "watermark") {
    entry = watermarkText;
    textInput.value = entry;
  }
  canvas.style.cursor = tool === "drag" ? "move" : tool === "text" ? "text" : "crosshair";
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
  const current = color.toLowerCase();
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
  color = normalized;
  if (remember) {
    recentColors = rememberColor(normalized);
    renderPalette();
  } else {
    updatePaletteSelection();
  }
  if (inlineEditor) inlineEditor.element.style.color = color;
}

function updateWidthValue(): void {
  widthInput.value = String(penWidth);
  widthValue.textContent = String(penWidth);
}

function updateToolbarState(): void {
  const undoButton = toolbar.querySelector<HTMLButtonElement>('button[data-tool="undo"]');
  if (undoButton) undoButton.disabled = !engine?.canUndo();
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
  toolbar.classList.toggle(
    "compact",
    viewportWidth() < 760 || (committedRect.width ?? 0) < 320,
  );
  const rect = committedRect;
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
  updateSizeBadge(rect, toolbarAbove);
}

function showToolbar(): void {
  toolbar.hidden = false;
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
    updateSizeBadge(committedRect, toolbar.getBoundingClientRect().top < committedRect.y);
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
  updateToolbarState();
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

function loadSettings(): Promise<void> {
  return settings()
    .then((current) => {
      afterCapture = normalizeAfterCapture(current.afterCapture);
    })
    .catch(() => {
      afterCapture = "inplace";
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

function handleCapture(payload: OverlayCapture | null | undefined): void {
  if (!payload || !payload.url) return;
  if (phase !== "preparing") return;
  if (captureHandled) return;
  if (payload.id && payload.id === handledCaptureId) return;
  captureHandled = true;
  handledCaptureId = payload.id ?? "capture";
  void beginAnnotation(payload);
}

async function fetchCaptureBlob(url: string): Promise<Blob> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`图片请求失败（HTTP ${response.status}）`);
  return response.blob();
}

/** Opens the inline text editor at a canvas point (text and tag tools). */
function openInlineText(point: { x: number; y: number }): void {
  if (phase !== "annotating" || !engine || inlineEditor) return;
  const tool = currentTool;
  if (tool !== "text" && tool !== "tag") return;
  const shapeColor = color;
  const shapeWidth = penWidth;
  const displayFontSize = tool === "tag" ? 20 : shapeWidth * 3 + 16;
  inlineEditor = new InlineTextEditor({
    canvas,
    point,
    color: shapeColor,
    displayFontSize,
    placeholder: tool === "tag" ? "输入标签" : "输入文字",
    singleLine: tool === "tag",
    onSubmit: (text) => {
      inlineEditor = null;
      engine?.addShape({
        kind: tool,
        x: point.x,
        y: point.y,
        x2: point.x,
        y2: point.y,
        color: shapeColor,
        width: shapeWidth,
        text,
      });
      updateToolbarState();
    },
    onCancel: () => {
      inlineEditor = null;
    },
  });
}

async function beginAnnotation(payload: OverlayCapture): Promise<void> {
  try {
    await settingsReady;
    const blob = await fetchCaptureBlob(payload.url);
    const autoAction = mappedAfterCapture();
    if (autoAction) {
      // afterCapture copy/pin: skip the toolbar, finish with the raw PNG.
      const rawBase64 = await blobToBase64(blob);
      phase = "annotating";
      setToolbarStatus(PENDING_LABELS[autoAction.ui]);
      await finishWith(autoAction.go, rawBase64, autoAction.ui);
      return;
    }

    const image = await decodeImageBlob(blob);
    const rect = payload.rect ?? committedRect ?? selectionRect();
    committedRect = rect;
    selection.classList.remove("capturing");
    positionSelection(rect);
    canvas.width = Math.max(1, Math.round(payload.width || rect.width));
    canvas.height = Math.max(1, Math.round(payload.height || rect.height));
    canvas.hidden = false;
    prepareState.hidden = true;
    dim.hidden = true;
    updateSizeBadge(rect);

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
    const displayedWidth = canvas.getBoundingClientRect().width || rect.width;
    engine.setScale(canvas.width / Math.max(1, displayedWidth));
    engine.setTool(currentTool);
    engine.reset();
    engine.redraw();

    phase = "annotating";
    hint.hidden = true;
    crosshairV.hidden = true;
    crosshairH.hidden = true;
    showToolbar();
    updateToolbarState();
  } catch (error) {
    const message = errorMessage(error);
    reset();
    showTransient(`标注准备失败：${message} · 请重新拖动选择`);
  }
}

async function prepareSelectionFor(rect: Rect): Promise<void> {
  phase = "preparing";
  toolbarMoved = false;
  // Go captures straight through the overlay window: hide the border, corner
  // marks, size badge, crosshair and hint so the captured image contains none
  // of our UI (the selection interior is already transparent).
  hideToolbar();
  selection.classList.add("capturing");
  prepareState.hidden = true;
  hint.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  sizeBadge.hidden = true;
  demoToast.hidden = true;
  try {
    const payload = await prepareSelection(rect);
    handleCapture(payload);
  } catch (error) {
    if (captureHandled) return;
    failPreparation(errorMessage(error));
  }
}

function failPreparation(message: string): void {
  reset();
  showTransient(`截取失败：${message} · 请重新拖动选择`);
}

// ---------------------------------------------------------------------------
// In-overlay failure guidance (no main window involved)
// ---------------------------------------------------------------------------

function hideCaptureError(): void {
  captureError.hidden = true;
}

function showCaptureFailure(rawMessage: string, permission: boolean): void {
  inlineEditor?.cancel();
  inlineEditor = null;
  phase = "finishing";
  hideToolbar();
  selection.classList.remove("capturing");
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
  uiAction: ToolbarAction = action as ToolbarAction,
): Promise<void> {
  if (phase === "finishing") return;
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
    await finishAnnotated(action, base64);
    // Go hides the overlay and performs the action; the next overlay:shown
    // (BeginRegion) resets this page.
  } catch (error) {
    const message = errorMessage(error);
    setButtonPending(button, false);
    if (!engine) {
      // The afterCapture shortcut path has no annotation surface to return to.
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
  if (phase !== "annotating") return;
  if (action === "cancel") {
    await finishWith("cancel", "", "cancel");
    return;
  }
  if (!engine) return;
  await finishWith(action, engine.toPNGBase64(), action);
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
  phase = "idle";
  if (inlineEditor) {
    const editor = inlineEditor;
    inlineEditor = null;
    editor.cancel();
  }
  hideCaptureError();
  selection.classList.remove("capturing");
  if (transientTimer) window.clearTimeout(transientTimer);
  if (demoResetTimer) window.clearTimeout(demoResetTimer);
  transientTimer = undefined;
  demoResetTimer = undefined;
  engine = null;
  captureHandled = false;
  handledCaptureId = "";
  drawingPointer = null;
  committedRect = null;
  startX = 0;
  startY = 0;
  currentX = 0;
  currentY = 0;
  dim.hidden = false;
  selection.hidden = true;
  canvas.hidden = true;
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

function beginSelection(x: number, y: number): void {
  phase = "selecting";
  selection.classList.remove("capturing");
  startX = x;
  startY = y;
  currentX = x;
  currentY = y;
  hint.hidden = true;
  demoToast.hidden = true;
  canvas.hidden = true;
  prepareState.hidden = true;
  hideToolbar();
  moveCrosshair(x, y);
  renderSelection();
}

function cancelRegionFlow(): void {
  if (phase === "preparing" || phase === "finishing") return;
  if (transientTimer) window.clearTimeout(transientTimer);
  phase = "finishing";
  hideToolbar();
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
  void cancelRegion().catch(() => reset());
}

// ---------------------------------------------------------------------------
// Pointer & keyboard
// ---------------------------------------------------------------------------

function canvasPoint(event: PointerEvent): { x: number; y: number } | null {
  const rect = canvas.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) return null;
  const x = ((event.clientX - rect.left) * canvas.width) / rect.width;
  const y = ((event.clientY - rect.top) * canvas.height) / rect.height;
  if (x < -4 || y < -4 || x > canvas.width + 4 || y > canvas.height + 4) return null;
  return { x: clamp(x, 0, canvas.width), y: clamp(y, 0, canvas.height) };
}

window.addEventListener("pointerdown", (event) => {
  if (isInsideTextEditor(event.target)) return;
  if (isInsidePalette(event.target)) return;
  if (phase === "annotating") {
    if (isInsideToolbar(event.target)) return;
    if (event.button !== 0 || !engine) return;
    const point = canvasPoint(event);
    if (!point) return;
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
    drawingPointer = event.pointerId;
    try {
      canvas.setPointerCapture(event.pointerId);
    } catch {
      // Synthetic events (E2E) have no capturable pointer id; window listeners
      // still receive the moves.
    }
    engine.beginDraw(point);
    return;
  }
  if (phase !== "idle" && phase !== "selecting") return;
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
  beginSelection(event.clientX, event.clientY);
});

window.addEventListener("pointermove", (event) => {
  if (phase === "selecting") {
    currentX = clamp(event.clientX, 0, viewportWidth());
    currentY = clamp(event.clientY, 0, viewportHeight());
    moveCrosshair(currentX, currentY);
    renderSelection();
    return;
  }
  if (phase === "annotating" && drawingPointer !== null && engine) {
    const point = canvasPoint(event);
    if (point) engine.moveDraw(point);
  }
});

window.addEventListener("pointerup", (event) => {
  if (phase === "selecting" && event.button === 0) {
    const rect = selectionRect();
    if (rect.width < MIN_SELECTION || rect.height < MIN_SELECTION) {
      reset();
      return;
    }
    const finalRect = roundedSelectionRect();
    committedRect = finalRect;
    positionSelection(finalRect);
    void prepareSelectionFor(finalRect);
    return;
  }
  if (phase === "annotating" && drawingPointer !== null && engine) {
    engine.endDraw();
    drawingPointer = null;
    updateToolbarState();
  }
});

window.addEventListener("contextmenu", (event) => {
  event.preventDefault();
  if (phase === "idle" || phase === "selecting") cancelRegionFlow();
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
  if (event.key === "Escape") {
    event.preventDefault();
    if (phase === "annotating") {
      void finishWith("cancel", "");
      return;
    }
    if (phase === "idle" || phase === "selecting") {
      cancelRegionFlow();
      return;
    }
    return;
  }
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "z") {
    if (phase !== "annotating") return;
    event.preventDefault();
    undo();
    return;
  }
  if (event.key === "Enter" && phase === "annotating") {
    if (palettePanel.isOpen() && palettePanel.contains(event.target)) return;
    event.preventDefault();
    void runAction("copy");
  }
});

// ---------------------------------------------------------------------------
// Events / init
// ---------------------------------------------------------------------------

onOverlayCapture((payload) => handleCapture(payload));

onCaptureFailed((payload) => {
  // Go reports capture failures to the overlay page now: stay here and show
  // in-place guidance instead of involving the main window.
  showCaptureFailure(payload.message, payload.permission);
});

captureErrorSettings.addEventListener("click", () => {
  void openPermissionSettings().catch(() => {});
});
captureErrorRetry.addEventListener("click", () => void retryCapture());
captureErrorCancel.addEventListener("click", () => dismissCaptureFailure());
onOverlayShown((payload) => {
  if (payload && Number.isFinite(payload.width) && payload.width > 0) displayInfo = payload;
  // PrepareSelection re-shows the overlay without overlay:shown; if this is a
  // new BeginRegion session, reset everything.
  if (phase === "preparing" || phase === "annotating") return;
  settingsReady = loadSettings();
  reset();
});

buildToolbar();
updateWidthValue();
setColor(color);
reset();
settingsReady = loadSettings();
void overlayInfo()
  .then((info) => {
    if (info && Number.isFinite(info.width) && info.width > 0) displayInfo = info;
  })
  .catch(() => {
    displayInfo = null;
  });
