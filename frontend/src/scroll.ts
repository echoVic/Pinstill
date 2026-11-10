// The scrolling-capture control bar (Round 4, independent window).
//
// Go hides the region overlay and shows this small frameless window while it
// captures and stitches frames. The page only drives the session UI:
// - `scroll:init {rect, auto, accessibilityGranted}` configures the bar;
// - `scroll:progress {frames, height, status, auto}` updates progress/status;
// - `Capture.SetScrollAuto(enabled)` toggles automatic scrolling (Go returns an
//   error without macOS Accessibility permission; the page shows guidance and
//   offers a button for `Capture.OpenScrollAccessibilitySettings()`);
// - the action buttons call `Capture.FinishScroll(action)` and Go hides this
//   window afterwards.
//
// In a plain browser (no MyGo runtime) the bar runs a simulated session so the
// page remains demoable without errors.

import {
  finishScroll,
  hasNative,
  onScrollInit,
  onScrollProgress,
  openScrollAccessibilitySettings,
  scrollAccessibilityGranted,
  setScrollAuto,
  type FinishAnnotatedAction,
  type ScrollProgress,
} from "./bridge";
import { flashButtonSuccess, setButtonPending } from "./util";

type ScrollUiAction = "copy" | "save" | "pin" | "cancel";

const ACTION_DEFS: Array<{ action: ScrollUiAction; label: string; className?: string }> = [
  { action: "copy", label: "复制", className: "primary" },
  { action: "save", label: "保存" },
  { action: "pin", label: "贴图" },
  { action: "cancel", label: "取消", className: "cancel" },
];

const PENDING_TEXT: Record<ScrollUiAction, string> = {
  copy: "正在复制长图…",
  save: "正在保存长图…",
  pin: "正在固定长图…",
  cancel: "已取消，正在关闭…",
};

const DEMO_TEXT: Record<ScrollUiAction, string> = {
  copy: "复制长图到剪贴板",
  save: "打开保存对话框",
  pin: "创建桌面贴图",
  cancel: "取消本次滚动截图",
};

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const bar = el("scrollBar");
const progressEl = el("scrollProgress");
const hint = el("scrollHint");
const permissionButton = el<HTMLButtonElement>("scrollPermission");
const autoInput = el<HTMLInputElement>("scrollAuto");
const actionsHost = el("scrollActions");

let auto = false;
let accessibilityGranted = false;
let progress: ScrollProgress | null = null;
let busy = false;
let noticeTimer: number | undefined;
let demoTimer: number | undefined;

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  return "未知错误";
}

function buildActions(): void {
  actionsHost.replaceChildren();
  for (const def of ACTION_DEFS) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `scroll-action${def.className ? ` ${def.className}` : ""}`;
    button.dataset.scrollAction = def.action;
    button.textContent = def.label;
    button.title = def.label;
    actionsHost.append(button);
  }
}

function setBusy(next: boolean): void {
  busy = next;
  bar.classList.toggle("busy", busy);
  for (const button of actionsHost.querySelectorAll<HTMLButtonElement>("button")) {
    button.disabled = busy;
  }
  autoInput.disabled = busy;
}

function actionButton(action: ScrollUiAction): HTMLButtonElement | null {
  return actionsHost.querySelector<HTMLButtonElement>(`button[data-scroll-action="${action}"]`);
}

function progressText(payload: ScrollProgress | null): string {
  if (!payload) return "正在准备滚动会话…";
  const base = `${payload.frames} 帧 · ${payload.height} px`;
  switch (payload.status) {
    case "running":
      return `正在拼接 · ${base}`;
    case "done":
      return `拼接完成 · ${base}`;
    case "stalled":
      return `等待滚动 · ${base}`;
    case "max":
      return `已达上限 · ${base}`;
    case "stopped":
      return `已停止 · ${base}`;
    default:
      return `正在拼接 · ${base}`;
  }
}

function renderProgress(): void {
  progressEl.dataset.status = progress?.status ?? "running";
  progressEl.textContent = progressText(progress);
}

function renderHint(): void {
  if (!permissionButton.hidden) return;
  hint.hidden = false;
  hint.dataset.kind = "";
  const status = progress?.status;
  if (status === "max") hint.textContent = "已达最大长度，请选择完成动作";
  else if (status === "done" || status === "stalled") {
    hint.textContent = "可继续滚动，或选择完成动作";
  } else if (auto) {
    hint.textContent = "自动滚动中…（可关闭开关或直接完成）";
  } else {
    hint.textContent = "请滚动目标窗口，Pinstill 会自动抓帧";
  }
}

function showNotice(text: string, kind: "info" | "warn" | "error" = "info"): void {
  progressEl.dataset.status = kind === "info" ? (progress?.status ?? "running") : "notice";
  progressEl.textContent = text;
  if (noticeTimer) window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(() => {
    noticeTimer = undefined;
    if (!busy) renderProgress();
  }, 2800);
}

function showPermission(message?: string): void {
  hint.hidden = true;
  permissionButton.hidden = false;
  if (message) permissionButton.title = message;
  showNotice("需要「辅助功能」权限才能自动滚动", "warn");
}

function hidePermission(): void {
  permissionButton.hidden = true;
  hint.hidden = false;
  renderHint();
}

function syncAutoInput(): void {
  autoInput.checked = auto;
}

// ---------------------------------------------------------------------------
// Session state
// ---------------------------------------------------------------------------

function applyInit(payload: unknown): void {
  const init = payload as { auto?: unknown; accessibilityGranted?: unknown } | null;
  auto = Boolean(init?.auto);
  accessibilityGranted = Boolean(init?.accessibilityGranted);
  progress = null;
  setBusy(false);
  permissionButton.hidden = true;
  hint.hidden = false;
  syncAutoInput();
  renderProgress();
  renderHint();
}

onScrollInit((payload) => applyInit(payload));

onScrollProgress((payload) => {
  if (!payload) return;
  progress = payload;
  if (typeof payload.auto === "boolean" && payload.auto !== auto) {
    auto = payload.auto;
    syncAutoInput();
  }
  if (!busy) {
    renderProgress();
    renderHint();
  }
});

// ---------------------------------------------------------------------------
// Automatic scrolling toggle
// ---------------------------------------------------------------------------

async function onAutoChange(): Promise<void> {
  if (busy) {
    syncAutoInput();
    return;
  }
  const desired = autoInput.checked;
  if (!hasNative) {
    auto = desired;
    renderHint();
    showNotice(desired ? "浏览器预览 · 自动滚动已开启（模拟）" : "浏览器预览 · 已切回手动滚动");
    return;
  }
  try {
    await setScrollAuto(desired);
    auto = desired;
    if (desired) accessibilityGranted = true;
    hidePermission();
    syncAutoInput();
    showNotice(desired ? "自动滚动已开启" : "自动滚动已关闭");
  } catch (error) {
    syncAutoInput();
    showPermission(errorMessage(error));
  }
}

autoInput.addEventListener("change", () => void onAutoChange());

permissionButton.addEventListener("click", async () => {
  try {
    await openScrollAccessibilitySettings();
  } catch {
    // The dialog may still open; keep the guidance visible.
  }
  showNotice("已打开系统设置，请为 Pinstill 勾选「辅助功能」");
  window.setTimeout(async () => {
    const granted = await scrollAccessibilityGranted().catch(() => false);
    accessibilityGranted = granted;
    if (granted) {
      hidePermission();
      showNotice("已获得辅助功能权限，可再次开启自动滚动");
    }
  }, 1200);
});

// ---------------------------------------------------------------------------
// Finish actions
// ---------------------------------------------------------------------------

async function runAction(uiAction: ScrollUiAction): Promise<void> {
  if (busy) return;
  const button = actionButton(uiAction);
  setButtonPending(button, true);
  busy = true;
  setBusy(true);
  if (noticeTimer) {
    window.clearTimeout(noticeTimer);
    noticeTimer = undefined;
  }
  progressEl.dataset.status = "running";
  progressEl.textContent = PENDING_TEXT[uiAction];
  const goAction: FinishAnnotatedAction = uiAction;
  if (!hasNative) {
    demoFinish(uiAction);
    return;
  }
  try {
    await finishScroll(goAction);
    // Go hides this window and performs the action; nothing to reset here.
  } catch (error) {
    busy = false;
    setButtonPending(button, false);
    setBusy(false);
    renderProgress();
    showNotice(`操作失败：${errorMessage(error)}`, "error");
  }
}

function demoFinish(uiAction: ScrollUiAction): void {
  if (demoTimer) {
    window.clearInterval(demoTimer);
    demoTimer = undefined;
  }
  progressEl.dataset.status = "done";
  progressEl.textContent = `浏览器预览 · 会${DEMO_TEXT[uiAction]}`;
  window.setTimeout(() => {
    busy = false;
    const button = actionButton(uiAction);
    setButtonPending(button, false);
    setBusy(false);
    if (uiAction !== "cancel") flashButtonSuccess(button);
    renderProgress();
    renderHint();
    startDemo();
  }, 1900);
}

actionsHost.addEventListener("click", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest<HTMLButtonElement>("button[data-scroll-action]");
  if (!button || button.disabled) return;
  void runAction(button.dataset.scrollAction as ScrollUiAction);
});

window.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  void runAction("cancel");
});

// ---------------------------------------------------------------------------
// Browser-preview simulation
// ---------------------------------------------------------------------------

function startDemo(): void {
  if (hasNative) return;
  if (demoTimer) window.clearInterval(demoTimer);
  let frames = 0;
  let height = 0;
  auto = false;
  accessibilityGranted = false;
  setBusy(false);
  syncAutoInput();
  permissionButton.hidden = true;
  hint.hidden = false;
  progress = { frames, height, status: "running", auto: false };
  renderProgress();
  renderHint();
  demoTimer = window.setInterval(() => {
    if (busy) return;
    frames += 1;
    height += 340 + Math.floor(Math.random() * 60);
    const status: ScrollProgress["status"] = frames >= 6 ? "done" : "running";
    progress = { frames, height, status, auto };
    renderProgress();
    renderHint();
    if (status === "done" && demoTimer) {
      window.clearInterval(demoTimer);
      demoTimer = undefined;
    }
  }, 700);
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

buildActions();
setBusy(false);
syncAutoInput();
hint.hidden = false;
renderHint();
if (!hasNative) {
  startDemo();
} else {
  progressEl.dataset.status = "running";
  progressEl.textContent = "正在准备滚动会话…";
}
