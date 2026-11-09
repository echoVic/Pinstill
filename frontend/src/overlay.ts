// The region-selection overlay: a full-display transparent window dimmed by a
// mask, with the selection region punched out, a crosshair and a size badge.
//
// Esc (or a right click) cancels; a mouse-up selection larger than 3px finishes
// through `Capture.FinishRegion` with overlay-local CSS pixels. The page resets
// itself on every `overlay:shown` event so repeated captures start clean.

import {
  cancelRegion,
  finishRegion,
  onOverlayShown,
  overlayInfo,
  type OverlayInfo,
  type Rect,
} from "./bridge";

const MIN_SELECTION = 3;

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

let displayInfo: OverlayInfo | null = null;
let dragging = false;
let finishing = false;
let startX = 0;
let startY = 0;
let currentX = 0;
let currentY = 0;

function viewportWidth(): number {
  return displayInfo?.width && displayInfo.width > 0 ? displayInfo.width : window.innerWidth;
}

function viewportHeight(): number {
  return displayInfo?.height && displayInfo.height > 0 ? displayInfo.height : window.innerHeight;
}

function selectionRect(): Rect {
  return {
    x: Math.min(startX, currentX),
    y: Math.min(startY, currentY),
    width: Math.abs(currentX - startX),
    height: Math.abs(currentY - startY),
  };
}

function placeBadge(rect: Rect): void {
  const margin = 8;
  const badgeWidth = sizeBadge.offsetWidth || 92;
  const badgeHeight = sizeBadge.offsetHeight || 26;
  let left = rect.x + rect.width - badgeWidth;
  let top = rect.y + rect.height + margin;
  if (top + badgeHeight > window.innerHeight - margin) {
    top = rect.y - badgeHeight - margin;
  }
  if (top < margin) top = Math.max(margin, rect.y + margin);
  left = Math.min(Math.max(margin, left), window.innerWidth - badgeWidth - margin);
  sizeBadge.style.left = `${left}px`;
  sizeBadge.style.top = `${top}px`;
}

function showSelection(): void {
  const rect = selectionRect();
  selection.hidden = false;
  dim.hidden = true;
  selection.style.left = `${rect.x}px`;
  selection.style.top = `${rect.y}px`;
  selection.style.width = `${rect.width}px`;
  selection.style.height = `${rect.height}px`;
  sizeBadge.textContent = `${Math.round(rect.width)} × ${Math.round(rect.height)}`;
  sizeBadge.hidden = false;
  placeBadge(rect);
}

function moveCrosshair(x: number, y: number): void {
  crosshairV.hidden = false;
  crosshairH.hidden = false;
  crosshairV.style.left = `${x}px`;
  crosshairH.style.top = `${y}px`;
}

function reset(): void {
  dragging = false;
  finishing = false;
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
  el("hint").style.display = "";
  document.body.style.cursor = "crosshair";
}

async function cancel(): Promise<void> {
  if (finishing) return;
  finishing = true;
  dragging = false;
  selection.hidden = true;
  sizeBadge.hidden = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = true;
  try {
    await cancelRegion();
  } catch {
    // The native side may already have restored the main window; keep quiet.
  }
}

async function finish(rect: Rect): Promise<void> {
  finishing = true;
  crosshairV.hidden = true;
  crosshairH.hidden = true;
  hint.hidden = true;
  // Clamp to the display the overlay window covers, in overlay-local CSS px.
  const clampedX = Math.max(0, Math.min(viewportWidth(), rect.x));
  const clampedY = Math.max(0, Math.min(viewportHeight(), rect.y));
  const payload: Rect = {
    x: Math.round(clampedX),
    y: Math.round(clampedY),
    width: Math.round(Math.min(rect.width, viewportWidth() - clampedX)),
    height: Math.round(Math.min(rect.height, viewportHeight() - clampedY)),
  };
  try {
    await finishRegion(payload);
  } catch {
    // Never leave the overlay stuck in a finishing state.
    reset();
  }
}

window.addEventListener("pointerdown", (event) => {
  if (finishing) return;
  if (event.button !== 0) {
    event.preventDefault();
    void cancel();
    return;
  }
  event.preventDefault();
  dragging = true;
  startX = event.clientX;
  startY = event.clientY;
  currentX = event.clientX;
  currentY = event.clientY;
  hint.hidden = true;
  try {
    document.documentElement.setPointerCapture(event.pointerId);
  } catch {
    // Pointer capture is a nicety; window listeners still see the drag.
  }
  moveCrosshair(event.clientX, event.clientY);
  showSelection();
});

window.addEventListener("pointermove", (event) => {
  if (finishing || !dragging) return;
  currentX = Math.max(0, Math.min(window.innerWidth, event.clientX));
  currentY = Math.max(0, Math.min(window.innerHeight, event.clientY));
  moveCrosshair(currentX, currentY);
  showSelection();
});

window.addEventListener("pointerup", (event) => {
  if (!dragging || finishing || event.button !== 0) return;
  dragging = false;
  const rect = selectionRect();
  if (rect.width >= MIN_SELECTION && rect.height >= MIN_SELECTION) {
    void finish(rect);
    return;
  }
  // A click without a drag: keep the overlay open and start over.
  startX = 0;
  startY = 0;
  currentX = 0;
  currentY = 0;
  hint.hidden = false;
  showSelection();
  selection.hidden = true;
  sizeBadge.hidden = true;
  dim.hidden = false;
});

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  void cancel();
});

window.addEventListener("contextmenu", (event) => {
  event.preventDefault();
  void cancel();
});

onOverlayShown((payload) => {
  if (payload && Number.isFinite(payload.width) && payload.width > 0) displayInfo = payload;
  reset();
});

reset();
void overlayInfo()
  .then((info) => {
    displayInfo = info;
    return displayInfo;
  })
  .catch(() => {
    // Browser preview has no native display info; window coordinates suffice.
    displayInfo = null;
  });
