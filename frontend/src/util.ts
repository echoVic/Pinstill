// Small browser helpers shared by the Pinstill pages. Nothing here talks to Go:
// native calls live in bridge.ts only.

/** Encodes bytes as plain base64, without the `data:` prefix. */
export function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return btoa(binary);
}

/** Decodes plain base64 into bytes. */
export function base64ToBytes(base64: string): Uint8Array {
  const binary = atob(base64.replace(/\s+/g, ""));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

/** Reads a Blob as plain base64, without the `data:` prefix. */
export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("failed to read blob"));
    reader.onload = () => {
      const result = typeof reader.result === "string" ? reader.result : "";
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.readAsDataURL(blob);
  });
}

/**
 * Loads an image supplied by Go (`shot://…`, `http://shot.localhost/…` or a
 * `data:` URL in the browser preview) into something drawable on a canvas.
 * The fetched bytes are decoded from a Blob, so the canvas stays origin-clean
 * and mosaicking / export keep working.
 */
export async function decodeImageBlob(
  blob: Blob,
): Promise<ImageBitmap | HTMLImageElement> {
  if (typeof createImageBitmap === "function") {
    try {
      return await createImageBitmap(blob);
    } catch {
      // Fall through to an <img> for webviews without a Blob decoder.
    }
  }
  const objectURL = URL.createObjectURL(blob);
  const image = new Image();
  image.src = objectURL;
  await image.decode();
  return image;
}

export async function loadCanvasImage(url: string): Promise<ImageBitmap | HTMLImageElement> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`图片请求失败（HTTP ${response.status}）`);
  return decodeImageBlob(await response.blob());
}

/** Triggers a browser download; used only by the browser-preview fallback. */
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/** Human readable file size, e.g. "1.2 MB". */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const exponent = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  const value = bytes / 1024 ** exponent;
  return `${value >= 10 || exponent === 0 ? Math.round(value) : value.toFixed(1)} ${units[exponent]}`;
}

// ---------------------------------------------------------------------------
// Button feedback states (R7.3): a pending badge while a native action runs and
// a short ✓ badge when it succeeded. The page CSS owns the visuals
// (`.is-pending` / `.is-success`), this only keeps the DOM state honest.
// ---------------------------------------------------------------------------

const feedbackTimers = new WeakMap<HTMLElement, number>();
const priorDisabled = new WeakMap<HTMLElement, boolean>();

function feedbackTarget(target: HTMLElement | null | undefined): HTMLButtonElement | null {
  return target instanceof HTMLButtonElement ? target : null;
}

/** Shows/hides the pending badge and disables the button while work runs. */
export function setButtonPending(
  target: HTMLElement | null | undefined,
  pending: boolean,
): void {
  const button = feedbackTarget(target);
  if (!button) return;
  if (pending) {
    // Remember the original disabled state only once per pending run, so
    // repeated calls cannot leave the button stuck disabled.
    if (!button.classList.contains("is-pending")) priorDisabled.set(button, button.disabled);
    button.disabled = true;
    button.classList.add("is-pending");
    button.setAttribute("aria-busy", "true");
    return;
  }
  button.classList.remove("is-pending");
  button.removeAttribute("aria-busy");
  button.disabled = priorDisabled.get(button) ?? false;
  priorDisabled.delete(button);
}

/** Flashes the ✓ success badge (same duration as the toast it usually follows). */
export function flashButtonSuccess(
  target: HTMLElement | null | undefined,
  duration = 900,
): void {
  const button = feedbackTarget(target);
  if (!button) return;
  const timer = feedbackTimers.get(button);
  if (timer) window.clearTimeout(timer);
  button.classList.remove("is-success");
  // Force a reflow so rapid repeats restart the pop animation.
  void button.offsetWidth;
  button.classList.add("is-success");
  feedbackTimers.set(
    button,
    window.setTimeout(() => {
      button.classList.remove("is-success");
      feedbackTimers.delete(button);
    }, duration),
  );
}

/** Runs `task` with the pending badge, then flashes ✓ unless it throws. */
export async function withButtonFeedback<T>(
  target: HTMLElement | null | undefined,
  task: () => Promise<T>,
  options: { flash?: boolean } = {},
): Promise<T> {
  const button = feedbackTarget(target);
  setButtonPending(button, true);
  try {
    const result = await task();
    setButtonPending(button, false);
    if (options.flash !== false) flashButtonSuccess(button);
    return result;
  } catch (error) {
    setButtonPending(button, false);
    throw error;
  }
}

/** Waits for the browser to paint the next frame. */
export function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}
