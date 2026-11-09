// The region-selection overlay: a full-display transparent window dimmed by a
// mask, with the selection region punched out, a crosshair, a size badge and —
// the desktop-first Round 2 flow — an in-place action bar next to the finished
// selection: 标注 / 复制 / 贴图 / 保存 / 取消.
//
// A finished selection stays on screen (no jump to the main window); the chosen
// action goes through `Capture.CompleteSelection`. When the user's
// `afterCapture` setting is not "toolbar", the matching action runs
// automatically ~250ms after the mouse-up. Esc/right-click cancels. The page
// resets itself on every `overlay:shown` event so repeated captures start
// clean, and the browser preview simulates the actions without throwing.

import {
  cancelRegion,
  completeSelection,
  hasNative,
  onOverlayShown,
  overlayInfo,
  settings,
  type OverlayInfo,
  type Rect,
  type SelectionAction,
} from "./bridge";

const MIN_SELECTION = 3;
const ACTION_GAP = 10;
const EDGE_MARGIN = 10;

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const dim = el("dim");
const selection = el("selection");
const crosshairV = el("crosshairV");
const crosshairH = el("crosshairH");
const sizeBadge = el("sizeBadge");
const hint = el("hint");
const actionBar = el("actionBar");
const actionStatus = el("actionStatus");
const demoToast = el("demoToast");

type AfterCapture = "toolbar" | "clipboard" | "pin" | "editor";
type Phase = "idle" | "dragging" | "selected" | "pending" | "finishing";

const AUTO_ACTIONS: Record<string, SelectionAction | undefined> = {
  clipboard: "copy",
  pin: "pin",
  editor: "annotate",
};

const PENDING_LABELS: Record<SelectionAction, string> = {
  annotate: "正在打开编辑器…",
  copy: "正在复制…",
  pin: "正在固定到桌面…",
  save: "正在保存…",
};

const DEMO_MESSAGES: Record<SelectionAction, string> = {
  annotate: "标注：桌面应用中会截图并进入主窗口编辑器",
  copy: "复制：桌面应用中会写入系统剪贴板",
  pin: "贴图：桌面应用中会创建置顶的桌面贴图窗口",
  save: "保存：桌面应用中会打开保存对话框",
};

let displayInfo: OverlayInfo | null = null;
let phase: Phase = "idle";
let afterCapture: AfterCapture = "toolbar";
let settingsReady: Promise<void> = Promise.resolve();

let startX = 0;
let startY = 0;
let currentX = 0;
let currentY = 0;
let autoTimer: number | undefined;
let demoResetTimer: number | undefined;

function viewportWidth(): number {
  return displayInfo?.width && displayInfo.width > 0 ? displayInfo.width : window.innerWidth;
}

function viewportHeight(): number {
  return displayInfo?.height && displayInfo.height > 0 ? displayInfo.height : window.innerHeight;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

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
  return {
    x: Math.round(rect.x),
    y: Math.round(rect.y),
    width: Math.round(rect.width),
    height: Math.round(rect.height),
  };
}

// ---------------------------------------------------------------------------
// Geometry & chrome
// ---------------------------------------------------------------------------

function moveCrosshair(x: number, y: number): void {
  crosshairV.hidden = false;
  crosshairH.hidden = false;
  crosshairV.style.left = `${x}px`;
  crosshairH.style.top = `${y}px`;
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

function updateSizeBadge(rect: Rect, barAbove = false): void {
  sizeBadge.textContent = `${Math.round(rect.width)} × ${Math.round(rect.height)}`;
  sizeBadge.hidden = false;
  const margin = EDGE_MARGIN;
  const badgeWidth = sizeBadge.offsetWidth || 84;
  const badgeHeight = sizeBadge.offsetHeight || 24;
  let left = rect.x + rect.width - badgeWidth;
  let top = barAbove ? rect.y + 8 : rect.y - badgeHeight - 8;
  if (top < margin) top = rect.y + 8;
  left = clamp(left, margin, Math.max(margin, viewportWidth() - badgeWidth - margin));
  top = clamp(top, margin, Math.max(margin, viewportHeight() - badgeHeight - margin));
  sizeBadge.style.left = `${left}px`;
  sizeBadge.style.top = `${top}px`;
}

function hideActionBar(): void {
  actionBar.hidden = true;
  actionStatus.textContent = "";
  actionStatus.dataset.kind = "";
  for (const button of actionBar.querySelectorAll<HTMLButtonElement>("button[data-action]")) {
    button.disabled = false;
  }
}

function placeActionBar(rect: Rect): void {
  const barWidth = actionBar.offsetWidth;
  const barHeight = actionBar.offsetHeight;
  const belowTop = rect.y + rect.height + ACTION_GAP;
  const aboveTop = rect.y - barHeight - ACTION_GAP;
  let top: number;
  const barAbove = belowTop + barHeight > viewportHeight() - EDGE_MARGIN && aboveTop >= EDGE_MARGIN;
  if (barAbove) {
    top = aboveTop;
  } else {
    top = belowTop;
  }
  top = clamp(top, EDGE_MARGIN, Math.max(EDGE_MARGIN, viewportHeight() - barHeight - EDGE_MARGIN));
  const left = clamp(
    rect.x + rect.width / 2 - barWidth / 2,
    EDGE_MARGIN,
    Math.max(EDGE_MARGIN, viewportWidth() - barWidth - EDGE_MARGIN),
  );
  actionBar.style.left = `${left}px`;
  actionBar.style.top = `${top}px`;
  // The size badge sits above the selection unless the bar is there.
  updateSizeBadge(rect, barAbove);
}

function showActionBar(): void {
  actionBar.hidden = false;
  actionBar.style.left = "0px";
  actionBar.style.top = "0px";
  placeActionBar(selectionRect());
}

function setBarPending(pending: boolean, label = "", kind: "info" | "error" = "info"): void {
  actionStatus.textContent = label;
  actionStatus.dataset.kind = kind;
  for (const button of actionBar.querySelectorAll<HTMLButtonElement>("button[data-action]")) {
    button.disabled = pending;
  }
  actionBar.setAttribute("aria-busy", String(pending));
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

function clearTimers(): void {
  if (autoTimer) window.clearTimeout(autoTimer);
  if (demoResetTimer) window.clearTimeout(demoResetTimer);
  autoTimer = undefined;
  demoResetTimer = undefined;
}

function reset(): void {
  clearTimers();
  phase = "idle";
  startX = 0;
  startY = 0;
  currentX = 0;
  currentY = 0;
  dim.hidden = false;
  selection.hidden = true;
  sizeBadge.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = false;
  demoToast.hidden = true;
  hideActionBar();
  document.body.style.cursor = "crosshair";
}

function normalizeAfterCapture(value: unknown): AfterCapture {
  return value === "clipboard" || value === "pin" || value === "editor" ? value : "toolbar";
}

function loadSettings(): Promise<void> {
  return settings()
    .then((current) => {
      afterCapture = normalizeAfterCapture(current.afterCapture);
    })
    .catch(() => {
      // The binding may not be implemented yet; the toolbar is the default.
      afterCapture = "toolbar";
    });
}

function beginSelection(x: number, y: number): void {
  clearTimers();
  phase = "dragging";
  startX = x;
  startY = y;
  currentX = x;
  currentY = y;
  hint.hidden = true;
  demoToast.hidden = true;
  hideActionBar();
  moveCrosshair(x, y);
  renderSelection();
}

function scheduleAutoAction(): void {
  if (afterCapture === "toolbar") return;
  const action = AUTO_ACTIONS[afterCapture];
  if (!action) return;
  autoTimer = window.setTimeout(() => {
    autoTimer = undefined;
    if (phase === "selected") void runAction(action);
  }, 250);
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

function runDemoAction(action: SelectionAction): void {
  demoToast.textContent = `浏览器预览 · ${DEMO_MESSAGES[action]}`;
  demoToast.hidden = false;
  setBarPending(false);
  actionStatus.textContent = "浏览器预览";
  demoResetTimer = window.setTimeout(() => reset(), 1800);
}

async function runAction(action: SelectionAction): Promise<void> {
  if (phase !== "selected") return;
  if (autoTimer) {
    window.clearTimeout(autoTimer);
    autoTimer = undefined;
  }
  phase = "pending";
  setBarPending(true, PENDING_LABELS[action]);
  if (!hasNative) {
    runDemoAction(action);
    return;
  }
  try {
    await completeSelection(roundedSelectionRect(), action);
    phase = "finishing";
    actionStatus.textContent = "完成";
    crosshairV.hidden = true;
    crosshairH.hidden = true;
  } catch (error) {
    phase = "selected";
    const message = error instanceof Error && error.message ? error.message : "未知错误";
    setBarPending(false, `操作失败：${message}`, "error");
  }
}

function cancel(): void {
  if (phase === "pending" || phase === "finishing") return;
  clearTimers();
  hideActionBar();
  selection.hidden = true;
  sizeBadge.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = true;
  demoToast.hidden = true;
  phase = "finishing";
  if (!hasNative) {
    reset();
    return;
  }
  void cancelRegion().catch(() => reset());
}

// ---------------------------------------------------------------------------
// Pointer & keyboard
// ---------------------------------------------------------------------------

function isInsideActionBar(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("#actionBar"));
}

window.addEventListener("pointerdown", (event) => {
  if (phase === "pending" || phase === "finishing") return;
  if (isInsideActionBar(event.target)) return;
  if (event.button !== 0) {
    event.preventDefault();
    cancel();
    return;
  }
  event.preventDefault();
  try {
    document.documentElement.setPointerCapture(event.pointerId);
  } catch {
    // Pointer capture is a nicety; window listeners still see the drag.
  }
  beginSelection(event.clientX, event.clientY);
});

window.addEventListener("pointermove", (event) => {
  if (phase !== "dragging") return;
  currentX = clamp(event.clientX, 0, viewportWidth());
  currentY = clamp(event.clientY, 0, viewportHeight());
  moveCrosshair(currentX, currentY);
  renderSelection();
});

window.addEventListener("pointerup", (event) => {
  if (phase !== "dragging" || event.button !== 0) return;
  const rect = selectionRect();
  if (rect.width < MIN_SELECTION || rect.height < MIN_SELECTION) {
    reset();
    return;
  }
  phase = "selected";
  renderSelection();
  showActionBar();
  setBarPending(false);
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  // Wait for Capture.Settings() once, so a fast drag cannot miss a
  // non-toolbar afterCapture configuration.
  void settingsReady.then(() => {
    if (phase === "selected") scheduleAutoAction();
  });
});

actionBar.addEventListener("click", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest<HTMLButtonElement>("button[data-action]");
  if (!button || button.disabled) return;
  const action = button.dataset.action;
  if (action === "cancel") {
    cancel();
    return;
  }
  if (action === "annotate" || action === "copy" || action === "pin" || action === "save") {
    void runAction(action);
  }
});

window.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    event.preventDefault();
    cancel();
    return;
  }
  if (event.key === "Enter" && phase === "selected") {
    event.preventDefault();
    void runAction("copy");
  }
});

window.addEventListener("contextmenu", (event) => {
  event.preventDefault();
  if (isInsideActionBar(event.target)) return;
  cancel();
});

onOverlayShown((payload) => {
  if (payload && Number.isFinite(payload.width) && payload.width > 0) displayInfo = payload;
  reset();
  settingsReady = loadSettings();
});

// Initial state: the mask is visible before any selection (selftest friendly).
reset();
settingsReady = loadSettings();
void overlayInfo()
  .then((info) => {
    if (info && Number.isFinite(info.width) && info.width > 0) displayInfo = info;
  })
  .catch(() => {
    displayInfo = null;
  });
