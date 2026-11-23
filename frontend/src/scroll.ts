// The scrolling-capture control bar (R9, compact 380x64 window).
//
// Flow: the overlay starts a manual session with `Capture.StartScroll(rect)`.
// Go hides the overlay and shows this control bar next to the selection. It
// captures frames while the user (or automatic scrolling) moves the target:
// - init and progress events carry a unique sessionId;
// - pause/resume: `Capture.SetScrollPaused(sessionId, bool)`;
// - auto/manual: `Capture.SetScrollAuto(sessionId, bool)` (permission denied → guidance);
// - finish/cancel: `Capture.FinishScroll(sessionId, "finish" | "cancel")`; Go stitches,
//   stores the long image and opens the result preview window / discards it.
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
  setScrollPaused,
  type ScrollProgress,
} from "./bridge";
import { flashButtonSuccess, setButtonPending } from "./util";

type ControlAction = "pause" | "mode" | "finish" | "cancel";

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const bar = el("scrollBar");
const statusEl = el("scrollStatus");
const progressEl = el("scrollProgress");
const permissionButton =
  document.querySelector<HTMLButtonElement>("[data-scroll-permission]") ??
  (() => {
    throw new Error("missing element [data-scroll-permission]");
  })();

let auto = false;
let paused = false;
let accessibilityGranted = false;
let denied = false;
let frames = 0;
let height = 0;
let status: ScrollProgress["status"] = "running";
let busy = false;
let sessionSequence = 0;
let sessionId = "";
const seenSessions = new Set<string>();
let latestNativeSession = 0n;
let noticeTimer: number | undefined;
let demoTimer: number | undefined;
let demoTick = 0;

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error) return error;
  return "未知错误";
}

function actionButton(action: ControlAction): HTMLButtonElement | null {
  return bar.querySelector<HTMLButtonElement>(`[data-scroll-action="${action}"]`);
}

function setBusy(next: boolean): void {
  busy = next;
  bar.classList.toggle("busy", busy);
  for (const button of bar.querySelectorAll<HTMLButtonElement>("button")) {
    if (button === permissionButton) continue;
    button.disabled = busy;
  }
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

function statusText(): string {
  if (status === "paused") return "已暂停";
  if (status === "max") return "已达上限";
  if (status === "stalled") return "等待滚动…";
  return auto ? "正在自动滚动…" : "手动滚动中…";
}

function render(): void {
  // A transient notice owns the status line until it expires.
  if (noticeTimer === undefined) {
    statusEl.dataset.status = status;
    statusEl.textContent = statusText();
  }
  progressEl.dataset.status = status;
  progressEl.textContent = `已拼接 ${frames} 帧 · ${height} px`;

  const pauseButton = actionButton("pause");
  if (pauseButton) {
    pauseButton.textContent = paused ? "继续" : "暂停";
    pauseButton.setAttribute("aria-pressed", String(paused));
    pauseButton.title = paused ? "继续抓帧" : "暂停抓帧";
  }
  const modeButton = actionButton("mode");
  if (modeButton) {
    modeButton.setAttribute("aria-pressed", String(auto));
    modeButton.title = auto ? "关闭自动滚动" : "开启自动滚动";
  }
  const finishButton = actionButton("finish");
  if (finishButton) finishButton.disabled = busy || frames === 0;
  permissionButton.hidden = !denied;
  permissionButton.textContent = "自动滚动需要辅助功能权限 · 去授权";
}

function showNotice(text: string, kind: "info" | "warn" | "error" = "info"): void {
  statusEl.dataset.status = kind === "info" ? "notice" : kind;
  statusEl.textContent = text;
  if (noticeTimer) window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(() => {
    noticeTimer = undefined;
    if (!busy) render();
  }, 2800);
}

function applyProgress(payload: ScrollProgress): void {
  if (!sessionId || payload.sessionId !== sessionId) return;
  frames = Math.max(0, payload.frames || 0);
  height = Math.max(0, payload.height || 0);
  status = payload.status;
  if (payload.status === "paused") paused = true;
  else if (payload.status === "running") paused = false;
  if (typeof payload.auto === "boolean") auto = payload.auto;
  if (!busy) render();
}

// ---------------------------------------------------------------------------
// Session init
// ---------------------------------------------------------------------------

function applyInit(payload: unknown): boolean {
  const init = payload as {
    sessionId?: unknown;
    auto?: unknown;
    accessibilityGranted?: unknown;
    autoDenied?: unknown;
  } | null;
  if (typeof init?.sessionId !== "string" || !init.sessionId || seenSessions.has(init.sessionId)) return false;
  const nativeSequence = /^scroll-(\d+)$/.exec(init.sessionId);
  if (nativeSequence) {
    // Go's uint64 sequence can exceed Number's exact integer range.
    const incoming = BigInt(nativeSequence[1]);
    if (incoming <= latestNativeSession) return false;
    latestNativeSession = incoming;
  } else {
    seenSessions.add(init.sessionId);
  }
  sessionId = init.sessionId;
  ++sessionSequence;
  stopDemo();
  if (noticeTimer) window.clearTimeout(noticeTimer);
  noticeTimer = undefined;
  for (const button of bar.querySelectorAll<HTMLButtonElement>("button.is-pending")) {
    setButtonPending(button, false);
  }
  auto = Boolean(init?.auto);
  accessibilityGranted = Boolean(init?.accessibilityGranted);
  denied = Boolean(init?.autoDenied);
  paused = false;
  frames = 0;
  height = 0;
  status = "running";
  setBusy(false);
  render();
  return true;
}

onScrollInit((payload) => {
  if (applyInit(payload) && !hasNative) runDemo();
});

onScrollProgress((payload) => {
  if (!payload) return;
  applyProgress(payload);
});

// ---------------------------------------------------------------------------
// Controls
// ---------------------------------------------------------------------------

async function togglePause(): Promise<void> {
  if (busy || !sessionId) return;
  const sequence = sessionSequence;
  const next = !paused;
  const button = actionButton("pause");
  if (button?.disabled) return;
  setButtonPending(button, true);
  try {
    await setScrollPaused(sessionId, next);
    if (sequence !== sessionSequence || busy) return;
    paused = next;
    status = next ? "paused" : "running";
    render();
    if (!hasNative) {
      showNotice(next ? "浏览器预览 · 已暂停抓帧（模拟）" : "浏览器预览 · 已继续抓帧（模拟）");
    }
  } catch (error) {
    if (sequence !== sessionSequence || busy) return;
    showNotice(`暂停/继续失败：${errorMessage(error)}`, "error");
  } finally {
    if (sequence === sessionSequence) {
      setButtonPending(button, false);
      if (button) button.disabled = busy;
    }
  }
}

async function toggleMode(): Promise<void> {
  if (busy || !sessionId) return;
  const sequence = sessionSequence;
  const desired = !auto;
  const button = actionButton("mode");
  if (button?.disabled) return;
  setButtonPending(button, true);
  try {
    await setScrollAuto(sessionId, desired);
    if (sequence !== sessionSequence || busy) return;
    auto = desired;
    if (desired) {
      accessibilityGranted = true;
      denied = false;
    }
    render();
    if (!hasNative) {
      showNotice(desired ? "浏览器预览 · 已切换到自动滚动" : "浏览器预览 · 已切换到手动滚动");
    } else {
      showNotice(desired ? "已切换到自动滚动" : "已切换到手动滚动");
    }
  } catch (error) {
    if (sequence !== sessionSequence || busy) return;
    // No Accessibility permission: keep manual mode and surface the guidance.
    auto = false;
    denied = true;
    render();
    showNotice("自动滚动需要「辅助功能」权限", "warn");
    permissionButton.title = errorMessage(error);
  } finally {
    if (sequence === sessionSequence) {
      setButtonPending(button, false);
      if (button) button.disabled = busy;
    }
  }
}

async function finish(cancel = false): Promise<void> {
  if (busy || !sessionId) return;
  const sequence = sessionSequence;
  const action = cancel ? "cancel" : "finish";
  const button = actionButton(action);
  busy = true;
  setBusy(true);
  if (noticeTimer) {
    window.clearTimeout(noticeTimer);
    noticeTimer = undefined;
  }
  if (cancel) {
    statusEl.dataset.status = "notice";
    statusEl.textContent = "已取消，正在关闭…";
  } else {
    statusEl.dataset.status = "running";
    statusEl.textContent = frames > 0 ? "正在生成预览…" : "还没有抓到帧…";
  }
  if (!hasNative) {
    demoFinish(cancel, button);
    return;
  }
  setButtonPending(button, true);
  try {
    await finishScroll(sessionId, action);
    // Go hides this window; on "finish" it opens the result preview window.
  } catch (error) {
    if (sequence !== sessionSequence) return;
    busy = false;
    setButtonPending(button, false);
    setBusy(false);
    render();
    showNotice(`操作失败：${errorMessage(error)}`, "error");
  }
}

permissionButton.addEventListener("click", async () => {
  const sequence = sessionSequence;
  try {
    await openScrollAccessibilitySettings();
  } catch {
    // The system dialog may still open; keep the guidance visible.
  }
  if (sequence !== sessionSequence || busy) return;
  showNotice("已打开系统设置，请为 Pinstill 勾选「辅助功能」");
  window.setTimeout(async () => {
    if (sequence !== sessionSequence || busy) return;
    const granted = await scrollAccessibilityGranted().catch(() => false);
    if (sequence !== sessionSequence || busy) return;
    accessibilityGranted = granted;
    if (granted) {
      denied = false;
      render();
      showNotice("已获得辅助功能权限，可切换到自动滚动", "info");
    }
  }, 1200);
});

bar.addEventListener("click", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest<HTMLButtonElement>("button[data-scroll-action]");
  if (!button || button.disabled) return;
  if (button.dataset.scrollAction === "pause") void togglePause();
  else if (button.dataset.scrollAction === "mode") void toggleMode();
  else if (button.dataset.scrollAction === "finish") void finish(false);
  else if (button.dataset.scrollAction === "cancel") void finish(true);
});

window.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  void finish(true);
});

// ---------------------------------------------------------------------------
// Browser-preview simulation
// ---------------------------------------------------------------------------

function startDemo(startDenied = false): void {
  if (hasNative) return;
  applyInit({ sessionId: `demo-${sessionSequence + 1}`, auto: false, autoDenied: startDenied, accessibilityGranted: !startDenied });
  runDemo();
  if (startDenied) {
    showNotice("浏览器预览 · 无辅助功能权限，已降级为手动滚动", "warn");
  }
}

function runDemo(): void {
  const id = sessionId;
  demoTick = 0;
  demoTimer = window.setInterval(() => {
    if (id !== sessionId || busy || paused) return;
    demoTick += 1;
    applyProgress({ sessionId: id, frames: frames + 1, height: height + 360 + (demoTick % 3) * 24,
      status: frames + 1 >= 6 ? "max" : "running", auto });
    if (frames >= 6) {
      stopDemo();
      showNotice("浏览器预览 · 已达上限，可点「立即完成」查看长图预览", "info");
    }
  }, 700);
}

function stopDemo(): void {
  if (demoTimer) {
    window.clearInterval(demoTimer);
    demoTimer = undefined;
  }
}

function demoFinish(cancel: boolean, button: HTMLButtonElement | null): void {
  const sequence = sessionSequence;
  stopDemo();
  window.setTimeout(() => {
    if (sequence !== sessionSequence) return;
    busy = false;
    setBusy(false);
    setButtonPending(button, false);
    if (cancel) {
      showNotice("浏览器预览 · 已取消本次滚动截图", "info");
      window.setTimeout(() => { if (sequence === sessionSequence) startDemo(denied); }, 1200);
      return;
    }
    if (frames === 0) {
      showNotice("还没有抓到帧，请先滚动目标窗口", "warn");
      startDemo(denied);
      return;
    }
    flashButtonSuccess(button);
    showNotice("浏览器预览 · 桌面应用中会打开长图预览窗口", "info");
    const query = new URLSearchParams({
      demo: "1",
      frames: String(frames),
      width: "1280",
      height: String(Math.max(1200, height)),
    });
    try {
      const opened = window.open(`./result.html?${query.toString()}`, "_blank");
      if (opened) document.documentElement.dataset.demoResultOpened = "1";
      else document.documentElement.dataset.demoResultOpened = "blocked";
    } catch {
      document.documentElement.dataset.demoResultOpened = "error";
    }
  }, cancel ? 450 : 700);
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

setBusy(false);
render();
if (hasNative) {
  statusEl.dataset.status = "running";
  statusEl.textContent = "正在准备滚动会话…";
} else {
  const params = new URLSearchParams(window.location.search);
  startDemo(params.get("autoDenied") === "1");
}
