// The pinned-image window: a frameless, always-on-top window showing one image
// the user asked to keep on screen. Go emits `pin:init` with the image and its
// presentation options; the page also accepts the same fields as query
// parameters so it can be opened for debugging before that event lands.
//
// Dragging uses the MyGo CSS property `--app-region: drag` (see pin.html); the
// close button is excluded with `no-drag`. There are no scrollbars.

import { closePin, hasNative } from "./bridge";
import { currentWindow, isMyGo, on } from "mygo-runtime";

interface PinInit {
  id?: string;
  url?: string;
  width?: number;
  height?: number;
  /** 30..100 */
  opacity?: number;
  border?: boolean;
}

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const windowEl = el("pinWindow");
const image = el<HTMLImageElement>("pinImage");
const errorEl = el("pinError");
const closeButton = el<HTMLButtonElement>("pinClose");

let pinId = "";
let closing = false;

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function showError(message: string): void {
  errorEl.textContent = message;
  errorEl.hidden = false;
  image.hidden = true;
}

function applyPin(init: PinInit): void {
  if (init.id) pinId = init.id;
  if (typeof init.opacity === "number" && Number.isFinite(init.opacity)) {
    // In a MyGo window Go applies the opacity natively (Window.SetOpacity), so
    // the page only mirrors it in the browser preview / query-parameter debug
    // path; applying both would compound the transparency.
    if (!hasNative) windowEl.style.opacity = String(clamp(init.opacity, 0, 100) / 100);
  }
  if (typeof init.border === "boolean") {
    windowEl.classList.toggle("bordered", init.border);
  }
  if (init.width && init.height) {
    document.title = `Tupen — 贴图 ${init.width} × ${init.height}`;
  }
  if (!init.url) {
    showError("贴图缺少图片地址");
    return;
  }
  image.hidden = false;
  errorEl.hidden = true;
  image.onerror = () => showError("无法载入贴图图片");
  image.src = init.url;
}

async function close(): Promise<void> {
  if (closing) return;
  closing = true;
  if (pinId && hasNative) {
    try {
      await closePin(pinId);
      return;
    } catch {
      // Fall through to closing the window itself.
    }
  }
  try {
    await currentWindow.close();
  } catch {
    window.close();
  }
}

closeButton.addEventListener("click", () => void close());
document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  void close();
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
