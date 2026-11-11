// The desktop pin window (Round 2): a frameless, always-on-top window that
// keeps one image on screen until the user closes it, Snipaste style.
//
// Page-side behavior from plan R2.2:
// - the whole image is the window's drag region (CSS `--app-region: drag`,
//   see pin.html); controls are no-drag;
// - custom context menu (copy / save / close / opacity
//   ±10% / original size), double-click copies, wheel scales the window
//   (⌘/Ctrl + wheel changes opacity), the bottom-right grip resizes the
//   window keeping the aspect ratio, Esc closes;
// - opacity is owned by Go in native mode (`SetPinOpacity` → Window.SetOpacity);
//   the page never stacks CSS opacity on top;
// - size/opacity changes are applied through ResizePin / SetPinOpacity and
//   persisted with a debounced UpdatePinState.
//
// The page also accepts the same fields as query parameters and simulates the
// behavior with toasts in a plain browser (hasNative === false), so the static
// preview stays demoable.

import {
  closePin,
  copyPin,
  hasNative,
  resizePin,
  savePin,
  setPinOpacity,
  updatePinState,
} from "./bridge";
import { currentWindow, isMyGo, on } from "mygo-runtime";

interface PinInit {
  id?: string;
  url?: string;
  width?: number;
  height?: number;
  /** 10..100, applied natively by Go. */
  opacity?: number;
  border?: boolean;
}

const MIN_WIDTH = 96;
const SCREEN_RATIO = 0.9;
const RESIZE_DEBOUNCE = 70;
const PERSIST_DEBOUNCE = 420;

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const windowEl = el("pinWindow");
const image = el<HTMLImageElement>("pinImage");
const closeButton = el<HTMLButtonElement>("pinClose");
const errorEl = el("pinError");
const resizeHandle = el("pinResize");
const menu = el("pinMenu");
const menuState = el("pinMenuState");
const toastEl = el("pinToast");

let pinId = "";
let closing = false;
let imageLoaded = false;
let menuOpen = false;

/** Tracked opacity (10..100); Go applies it in native mode. */
let opacity = 100;
/** Initial window size in DIP, from pin:init. */
let initialWidth = 0;
let initialHeight = 0;
/** Tracked window size in DIP (updated by wheel/grip/resizes). */
let currentWidth = 0;
let currentHeight = 0;
/** width / height, kept on every resize. */
let aspect = 1;

let resizeTimer: number | undefined;
let persistTimer: number | undefined;
let toastTimer: number | undefined;
let pendingWidth = 0;
let pendingHeight = 0;

let lastWheelScale = 0;
let lastWheelOpacity = 0;

let resizing = false;
let resizeStartX = 0;
let resizeStartW = 0;
let resizeStartH = 0;

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  return "未知错误";
}

function screenWidth(): number {
  return window.screen?.availWidth || window.innerWidth || 1440;
}

function screenHeight(): number {
  return window.screen?.availHeight || window.innerHeight || 900;
}

function showError(message: string): void {
  errorEl.textContent = message;
  errorEl.hidden = false;
  image.hidden = true;
}

function toast(message: string): void {
  toastEl.textContent = message;
  toastEl.hidden = false;
  if (toastTimer) window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => {
    toastEl.hidden = true;
    toastTimer = undefined;
  }, 1900);
}

function fitToScreen(width: number): { width: number; height: number } {
  const maxWidth = screenWidth() * SCREEN_RATIO;
  const maxHeight = screenHeight() * SCREEN_RATIO;
  let nextWidth = Math.max(MIN_WIDTH, width);
  let nextHeight = nextWidth / (aspect || 1);
  if (nextHeight > maxHeight) {
    nextHeight = maxHeight;
    nextWidth = nextHeight * (aspect || 1);
  }
  if (nextWidth > maxWidth) {
    nextWidth = maxWidth;
    nextHeight = nextWidth / (aspect || 1);
  }
  if (nextWidth < MIN_WIDTH) {
    nextWidth = MIN_WIDTH;
    nextHeight = nextWidth / (aspect || 1);
  }
  return { width: Math.round(nextWidth), height: Math.round(nextHeight) };
}

/** The image's size in DIP: image pixels / device scale, capped to the screen. */
function originalSize(): { width: number; height: number } | null {
  if (!imageLoaded || !image.naturalWidth || !image.naturalHeight) return null;
  const dpr = window.devicePixelRatio || 1;
  const width = image.naturalWidth / dpr;
  const height = image.naturalHeight / dpr;
  aspect = width / height;
  return fitToScreen(width);
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

function schedulePersist(): void {
  if (!hasNative || !pinId) return;
  if (persistTimer) window.clearTimeout(persistTimer);
  persistTimer = window.setTimeout(() => {
    persistTimer = undefined;
    void persistState();
  }, PERSIST_DEBOUNCE);
}

async function persistState(): Promise<void> {
  if (!hasNative || !pinId) return;
  const state = {
    width: Math.round(currentWidth || initialWidth),
    height: Math.round(currentHeight || initialHeight),
    opacity: Math.round(opacity),
  };
  try {
    await updatePinState(pinId, state);
  } catch {
    // Go persists position itself; a failed state sync is not fatal.
  }
}

function flushPersist(): void {
  if (persistTimer) {
    window.clearTimeout(persistTimer);
    persistTimer = undefined;
  }
  void persistState();
}

// ---------------------------------------------------------------------------
// Native window operations (browser preview degrades to toasts)
// ---------------------------------------------------------------------------

function applyResize(width: number, height: number): void {
  currentWidth = Math.round(width);
  currentHeight = Math.round(height);
  if (!hasNative || !pinId) return;
  pendingWidth = currentWidth;
  pendingHeight = currentHeight;
  // Leading-edge throttle: continuous dragging resizes the window about every
  // RESIZE_DEBOUNCE ms with the latest target instead of waiting for a pause.
  if (resizeTimer) return;
  resizeTimer = window.setTimeout(() => {
    resizeTimer = undefined;
    void applyNativeResize(pendingWidth, pendingHeight);
  }, RESIZE_DEBOUNCE);
}

async function applyNativeResize(width: number, height: number): Promise<void> {
  if (!hasNative || !pinId) return;
  try {
    await resizePin(pinId, Math.round(width), Math.round(height));
    schedulePersist();
  } catch (error) {
    toast(`调整大小失败：${errorMessage(error)}`);
  }
}

async function flushResize(): Promise<void> {
  if (resizeTimer) {
    window.clearTimeout(resizeTimer);
    resizeTimer = undefined;
  }
  if (hasNative && pinId) {
    await applyNativeResize(
      currentWidth || initialWidth,
      currentHeight || initialHeight,
    );
  }
  flushPersist();
}

async function closeWindow(): Promise<void> {
  if (closing) return;
  closing = true;
  closeMenu();
  if (!hasNative) {
    toast("浏览器预览 · 桌面应用中会关闭贴图窗口");
    closing = false;
    return;
  }
  if (pinId) {
    try {
      await closePin(pinId);
      return;
    } catch {
      // Fall through to closing this window directly.
    }
  }
  try {
    await currentWindow.close();
  } catch {
    try {
      window.close();
    } catch {
      closing = false;
    }
  }
}

async function copyImage(): Promise<void> {
  if (!hasNative) {
    toast("浏览器预览 · 桌面应用中会复制贴图");
    return;
  }
  try {
    await copyPin(pinId);
    toast("已复制到剪贴板");
  } catch (error) {
    toast(`复制失败：${errorMessage(error)}`);
  }
}

async function saveImage(): Promise<void> {
  if (!hasNative) {
    toast("浏览器预览 · 桌面应用中会打开保存对话框");
    return;
  }
  try {
    const path = await savePin(pinId);
    if (path) toast(`已保存：${path}`);
  } catch (error) {
    toast(`保存失败：${errorMessage(error)}`);
  }
}

async function adjustOpacity(delta: number): Promise<void> {
  const previous = opacity;
  const next = clamp(Math.round(opacity + delta), 10, 100);
  if (next === previous) {
    toast(previous >= 100 ? "已经是最大不透明度" : "已经是最小不透明度");
    return;
  }
  opacity = next;
  updateMenuState();
  if (!hasNative) {
    windowEl.style.opacity = String(opacity / 100);
    toast(`浏览器预览 · 不透明度 ${opacity}%`);
    return;
  }
  try {
    await setPinOpacity(pinId, opacity);
    toast(`不透明度 ${opacity}%`);
    schedulePersist();
  } catch (error) {
    opacity = previous;
    updateMenuState();
    toast(`调整透明度失败：${errorMessage(error)}`);
  }
}

async function resetSize(): Promise<void> {
  const target = originalSize();
  if (!target) {
    toast("图片尚未载入");
    return;
  }
  if (!hasNative) {
    toast(`浏览器预览 · 桌面应用中会恢复为 ${target.width} × ${target.height}`);
    return;
  }
  currentWidth = target.width;
  currentHeight = target.height;
  await applyNativeResize(target.width, target.height);
  toast(`已恢复原始大小 ${target.width} × ${target.height}`);
}

// ---------------------------------------------------------------------------
// Context menu
// ---------------------------------------------------------------------------

function updateMenuState(): void {
  menuState.textContent = `桌面贴图 · 不透明度 ${Math.round(opacity)}%`;
}

function closeMenu(): void {
  if (!menuOpen) return;
  menuOpen = false;
  menu.hidden = true;
}

function openMenu(x: number, y: number): void {
  updateMenuState();
  menu.hidden = false;
  menu.style.left = "0px";
  menu.style.top = "0px";
  const rect = menu.getBoundingClientRect();
  const left = clamp(x, 4, Math.max(4, window.innerWidth - rect.width - 4));
  const top = clamp(y, 4, Math.max(4, window.innerHeight - rect.height - 4));
  menu.style.left = `${left}px`;
  menu.style.top = `${top}px`;
  menuOpen = true;
}

window.addEventListener(
  "contextmenu",
  (event) => {
    event.preventDefault();
    if (closing) return;
    const target = event.target;
    if (target instanceof Element && target.closest("#pinMenu")) return;
    openMenu(event.clientX, event.clientY);
  },
  true,
);

window.addEventListener(
  "pointerdown",
  (event) => {
    if (!menuOpen) return;
    const target = event.target;
    if (target instanceof Element && target.closest("#pinMenu")) return;
    closeMenu();
  },
  true,
);

window.addEventListener("blur", closeMenu);

menu.addEventListener("click", (event) => {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const item = target.closest<HTMLButtonElement>("button[data-menu-action]");
  if (!item) return;
  const action = item.dataset.menuAction;
  closeMenu();
  switch (action) {
    case "copy":
      void copyImage();
      return;
    case "save":
      void saveImage();
      return;
    case "opacity-up":
      void adjustOpacity(10);
      return;
    case "opacity-down":
      void adjustOpacity(-10);
      return;
    case "reset-size":
      void resetSize();
      return;
    case "close":
      void closeWindow();
      return;
    default:
      return;
  }
});

closeButton.addEventListener("click", () => void closeWindow());

// ---------------------------------------------------------------------------
// Wheel: scale the window, ⌘/Ctrl + wheel changes opacity
// ---------------------------------------------------------------------------

window.addEventListener(
  "wheel",
  (event) => {
    if (!imageLoaded || closing) return;
    event.preventDefault();
    const now = performance.now();
    if (event.ctrlKey || event.metaKey) {
      // Trackpads send dozens of wheel events per gesture; step opacity at most
      // every 120 ms so ⌘+scroll feels like a deliberate ±10% step.
      if (now - lastWheelOpacity < 120) return;
      lastWheelOpacity = now;
      void adjustOpacity(event.deltaY < 0 ? 10 : -10);
      return;
    }
    if (now - lastWheelScale < 90) return;
    lastWheelScale = now;
    if (menuOpen) closeMenu();
    const factor = event.deltaY < 0 ? 1.1 : 0.9;
    const base = currentWidth || initialWidth || image.naturalWidth;
    const sized = fitToScreen(base * factor);
    currentWidth = sized.width;
    currentHeight = sized.height;
    if (!hasNative) {
      toast(`浏览器预览 · 缩放至 ${sized.width} × ${sized.height}`);
      return;
    }
    applyResize(sized.width, sized.height);
    schedulePersist();
  },
  { passive: false },
);

// ---------------------------------------------------------------------------
// Resize grip (aspect ratio kept, native ResizePin)
// ---------------------------------------------------------------------------

resizeHandle.addEventListener("pointerdown", (event) => {
  if (closing || !imageLoaded || event.button !== 0) return;
  event.preventDefault();
  event.stopPropagation();
  closeMenu();
  resizing = true;
  resizeStartX = event.clientX;
  resizeStartW = currentWidth || initialWidth || image.naturalWidth;
  resizeStartH = currentHeight || initialHeight || image.naturalHeight;
  try {
    resizeHandle.setPointerCapture(event.pointerId);
  } catch {
    // Window listeners still see the drag.
  }
});

window.addEventListener(
  "pointermove",
  (event) => {
    if (!resizing) return;
    const width = resizeStartW + (event.clientX - resizeStartX);
    const height = width / (aspect || 1);
    const sized = fitToScreen(width);
    currentWidth = sized.width;
    currentHeight = sized.height;
    if (hasNative) applyResize(sized.width, sized.height);
  },
  true,
);

window.addEventListener("pointerup", () => {
  if (!resizing) return;
  resizing = false;
  if (hasNative) {
    void flushResize();
  } else {
    toast(
      `浏览器预览 · 桌面应用中会调整为 ${Math.round(currentWidth || resizeStartW)} × ${Math.round(currentHeight || resizeStartH)}`,
    );
  }
});

// ---------------------------------------------------------------------------
// Init / lifecycle
// ---------------------------------------------------------------------------

function applyPin(init: PinInit): void {
  if (init.id) pinId = init.id;
  if (typeof init.opacity === "number" && Number.isFinite(init.opacity)) {
    opacity = clamp(Math.round(init.opacity), 10, 100);
    // Native mode: Go applies it with Window.SetOpacity; only the browser
    // preview mirrors it in CSS.
    if (!hasNative) windowEl.style.opacity = String(opacity / 100);
  }
  if (typeof init.border === "boolean") {
    windowEl.classList.toggle("bordered", init.border);
  }
  if (init.width && init.height) {
    initialWidth = Math.round(init.width);
    initialHeight = Math.round(init.height);
    currentWidth = initialWidth;
    currentHeight = initialHeight;
    aspect = initialWidth / initialHeight;
    document.title = `Pinstill — 贴图 ${initialWidth} × ${initialHeight}`;
  }
  updateMenuState();
  if (!init.url) {
    showError("贴图缺少图片地址");
    return;
  }
  errorEl.hidden = true;
  image.hidden = false;
  image.onload = () => {
    imageLoaded = true;
    if (!initialWidth || !initialHeight) {
      initialWidth = Math.round(image.naturalWidth / (window.devicePixelRatio || 1));
      initialHeight = Math.round(image.naturalHeight / (window.devicePixelRatio || 1));
      currentWidth = initialWidth;
      currentHeight = initialHeight;
    }
    if (initialWidth && initialHeight) aspect = initialWidth / initialHeight;
    errorEl.hidden = true;
    updateMenuState();
  };
  image.onerror = () => showError("无法载入贴图图片");
  image.src = init.url;
}


window.addEventListener("dblclick", (event) => {
  if (closing) return;
  const target = event.target;
  if (
    target instanceof Element &&
    (target.closest("#pinMenu") || target.closest("#pinClose") || target.closest("#pinResize"))
  ) {
    return;
  }
  void copyImage();
});

window.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  if (menuOpen) {
    closeMenu();
    return;
  }
  void closeWindow();
});

// Native event: `pin:init` (raw name, untyped mygo-runtime `on`).
if (isMyGo()) {
  try {
    on<PinInit>("pin:init", (payload) => applyPin(payload));
  } catch {
    // Never let a missing runtime break the page.
  }
}

// Debug/fallback: /pin.html?id=…&url=…&width=…&height=…&opacity=…&border=1
const params = new URLSearchParams(window.location.search);
if (params.get("url")) {
  const boolParam = params.get("border");
  applyPin({
    id: params.get("id") ?? undefined,
    url: params.get("url") ?? undefined,
    width: Number(params.get("width")) || undefined,
    height: Number(params.get("height")) || undefined,
    opacity: params.has("opacity") ? Number(params.get("opacity")) : undefined,
    border: boolParam === null ? undefined : boolParam !== "0" && boolParam !== "false",
  });
}
