// The scrolling-capture result window (R9): preview of the stitched long image.
//
// Go opens/reuses a normal window with `/result.html` after an automatic or
// manual scrolling session completes, and emits `scroll:result`
// `{id,url,width,height,frames,capped,historyId}`. The page shows the long image
// at the window width (scrollable), plus copy / save / pin / close. Successful
// actions close the window because the image is already stored in history.
//
// In a plain browser (no MyGo runtime) the page runs with demo data so the
// static preview stays clickable without console errors.

import {
  closeScrollResult,
  copyScrollResult,
  hasNative,
  onScrollResult,
  pinScrollResult,
  saveScrollResult,
  type ScrollResult,
} from "./bridge";
import { flashButtonSuccess, setButtonPending } from "./util";

const el = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const element = document.getElementById(id);
  if (!element) throw new Error(`missing element #${id}`);
  return element as T;
};

const windowEl = el("resultWindow");
const image = el<HTMLImageElement>("resultImage");
const loading = el("resultLoading");
const metaEl = el("resultMeta");
const statusEl = el("resultStatus");
const scrollArea = el("resultScroll");

let payload: ScrollResult | null = null;
let loaded = false;
let busy = false;
let closed = false;
let resultSequence = 0;

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error) return error;
  return "未知错误";
}

function setStatus(text: string, kind: "info" | "error" = "info"): void {
  statusEl.textContent = text;
  statusEl.dataset.kind = kind;
}

function formatMeta(result: ScrollResult): string {
  const frames = Math.max(0, Math.round(result.frames || 0));
  const width = Math.max(0, Math.round(result.width || 0));
  const height = Math.max(0, Math.round(result.height || 0));
  const parts = [`${width} × ${height} px`, `${frames} 帧`];
  if (result.capped) parts.push("已达上限");
  return parts.join(" · ");
}

// ---------------------------------------------------------------------------
// Result payload
// ---------------------------------------------------------------------------

function normalizeResult(source: Record<string, unknown> | null): ScrollResult | null {
  if (!source) return null;
  const url = typeof source.url === "string" ? source.url : "";
  if (!url) return null;
  const number = (value: unknown): number => {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
  };
  return {
    id: typeof source.id === "string" ? source.id : hasNative ? "" : "demo-result",
    url,
    width: number(source.width),
    height: number(source.height),
    frames: number(source.frames),
    capped: Boolean(source.capped),
    historyId: typeof source.historyId === "string" ? source.historyId : undefined,
  };
}

function applyResult(result: ScrollResult | null): void {
  if (!result) return;
  if (hasNative && !result.id.trim()) {
    loaded = true;
    loading.hidden = true;
    if (!payload) image.hidden = true;
    metaEl.textContent = "长图结果无效";
    setStatus("长图载入失败：缺少长图结果 ID", "error");
    return;
  }
  resultSequence++;
  payload = result;
  loaded = true;
  busy = false;
  closed = false;
  delete document.documentElement.dataset.resultClosed;
  windowEl.classList.remove("closing");
  document.querySelectorAll(".result-closed").forEach(note => note.remove());
  for (const button of actionButtons) {
    setButtonPending(button, false);
    button.classList.remove("is-success");
    button.disabled = false;
  }
  setStatus("");
  document.title = "Pinstill — 滚动截图预览";
  metaEl.textContent = formatMeta(result);
  loading.hidden = true;
  image.hidden = false;
  if (image.getAttribute("src") !== result.url) {
    image.src = result.url;
  }
  scrollArea.scrollTop = 0;
}

image.addEventListener("load", () => {
  loading.hidden = true;
  image.hidden = false;
  if (payload && (!payload.width || !payload.height) && image.naturalWidth > 0) {
    payload.width = image.naturalWidth;
    payload.height = image.naturalHeight;
    metaEl.textContent = formatMeta(payload);
  }
});

image.addEventListener("error", () => {
  loading.hidden = true;
  image.hidden = true;
  metaEl.textContent = "长图载入失败";
  setStatus("长图载入失败，请关闭后重试", "error");
});

// ---------------------------------------------------------------------------
// Window lifecycle
// ---------------------------------------------------------------------------

function closeLocally(reason = "窗口已关闭", sequence = resultSequence): void {
  if (closed || sequence !== resultSequence) return;
  const id = payload?.id ?? "";
  if (hasNative && !id.trim()) {
    setStatus("关闭失败：缺少长图结果 ID", "error");
    return;
  }
  closed = true;
  busy = true;
  document.documentElement.dataset.resultClosed = "1";
  windowEl.classList.add("closing");
  const note = document.createElement("div");
  note.className = "result-closed";
  const historyNote = payload?.historyId ? "长图已保存到截图历史" : "长图仅保留在本次预览";
  note.innerHTML = `<div><strong>${reason}</strong><span>${historyNote}</span></div>`;
  document.body.append(note);
  try {
    window.close();
  } catch {
    // Browsers block window.close() for pages that were not script-opened; the
    // note above keeps the demo understandable.
  }
  if (hasNative) {
    void closeScrollResult(id).catch(() => {});
  }
}

function dismiss(): void {
  closeLocally("预览已关闭");
}

function demoImageURL(height: number): string {
  const safeHeight = Math.min(9000, Math.max(1200, Math.round(height)));
  const blocks: string[] = [];
  const colors = ["#f3a24e", "#78a576", "#70a8c2", "#e46c4e", "#b8b1a5"];
  for (let index = 0; index < 6; index += 1) {
    const top = 220 + index * 340;
    if (top > safeHeight - 80) break;
    blocks.push(
      `<rect x="90" y="${top}" width="1100" height="18" rx="6" fill="${colors[index % colors.length]}" opacity=".8"/>`,
      `<rect x="90" y="${top + 34}" width="${820 - index * 40}" height="10" rx="5" fill="#a8a396"/>`,
      `<rect x="90" y="${top + 58}" width="${980 - index * 30}" height="10" rx="5" fill="#b8b3a6"/>`,
      `<rect x="90" y="${top + 96}" width="300" height="180" rx="12" fill="${colors[(index + 2) % colors.length]}" opacity=".35"/>`,
      `<rect x="420" y="${top + 96}" width="330" height="180" rx="12" fill="${colors[(index + 1) % colors.length]}" opacity=".3"/>`,
      `<rect x="780" y="${top + 96}" width="410" height="180" rx="12" fill="#d6c9ae" opacity=".55"/>`,
    );
  }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="${safeHeight}" viewBox="0 0 1280 ${safeHeight}">
<rect width="1280" height="${safeHeight}" fill="#f2efe7"/>
<rect width="1280" height="64" fill="#e4e0d6"/>
<circle cx="34" cy="32" r="6" fill="#d37d65"/><circle cx="56" cy="32" r="6" fill="#d2ae64"/><circle cx="78" cy="32" r="6" fill="#90ab86"/>
<text x="110" y="38" font-family="system-ui,sans-serif" font-size="15" fill="#6d6c63">团队工作台 / 长截图预览</text>
<text x="90" y="130" font-family="system-ui,sans-serif" font-size="42" font-weight="700" fill="#303a2e">把滚动的内容，一次带走。</text>
<text x="90" y="172" font-family="system-ui,sans-serif" font-size="18" fill="#73776b">Pinstill keeps the whole page in one image.</text>
<rect x="90" y="200" width="1100" height="1" fill="#d7d6c3"/>
${blocks.join("\n")}
<text x="90" y="${safeHeight - 40}" font-family="system-ui,sans-serif" font-size="13" fill="#8d887b">浏览器预览 · 示例长图</text>
</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

function demoResult(params: URLSearchParams): ScrollResult {
  const height = Number(params.get("height")) || 2400;
  const frames = Number(params.get("frames")) || 6;
  return {
    id: "demo-result",
    url: demoImageURL(height),
    width: Number(params.get("width")) || 1280,
    height: Math.min(9000, Math.max(1200, height)),
    frames,
    capped: params.get("capped") === "1",
  };
}

function queryResult(params: URLSearchParams): ScrollResult | null {
  return normalizeResult({
    id: params.get("id") ?? (hasNative ? "" : "demo-result"),
    url: params.get("url") ?? "",
    width: params.get("width") ?? "",
    height: params.get("height") ?? "",
    frames: params.get("frames") ?? "",
    capped: params.get("capped") === "1",
    historyId: params.get("historyId") ?? "",
  });
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

function demoDownload(): void {
  if (!payload) return;
  const anchor = document.createElement("a");
  anchor.href = payload.url;
  anchor.download = "Pinstill-scroll.svg";
  anchor.rel = "noopener";
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
}

const ACTION_LABELS: Record<string, string> = {
  copy: "复制",
  save: "保存",
  pin: "贴图",
  close: "关闭",
};

async function runAction(action: string, button: HTMLButtonElement | null): Promise<void> {
  if (action === "close") {
    dismiss();
    return;
  }
  if (busy || closed) return;
  const id = payload?.id ?? "";
  const sequence = resultSequence;
  const isCurrent = () => sequence === resultSequence && !closed;
  busy = true;
  let completed = false;
  setButtonPending(button, true);
  try {
    if (hasNative && !id.trim()) throw new Error("缺少长图结果 ID");
    if (!hasNative) {
      if (action === "save") demoDownload();
      setButtonPending(button, false);
      flashButtonSuccess(button);
      if (action === "copy") setStatus("浏览器预览 · 桌面应用中会复制长图到剪贴板");
      else if (action === "save") setStatus("浏览器预览 · 已下载示例长图");
      else if (action === "pin") setStatus("浏览器预览 · 桌面应用中会创建贴图窗口");
      completed = true;
      window.setTimeout(() => closeLocally("操作完成，窗口已自动关闭", sequence), 700);
      return;
    }
    if (action === "copy") {
      await copyScrollResult(id);
    } else if (action === "save") {
      const path = await saveScrollResult(id);
      if (!isCurrent()) return;
      setButtonPending(button, false);
      if (!path) {
        setStatus("已取消保存");
        return;
      }
    } else if (action === "pin") {
      await pinScrollResult(id);
    } else {
      return;
    }
    if (!isCurrent()) return;
    setButtonPending(button, false);
    flashButtonSuccess(button);
    setStatus(`已${ACTION_LABELS[action] ?? "完成"}，窗口将自动关闭…`);
    completed = true;
    window.setTimeout(() => closeLocally("操作完成，窗口已自动关闭", sequence), 320);
  } catch (error) {
    if (!isCurrent()) return;
    setButtonPending(button, false);
    setStatus(`${ACTION_LABELS[action] ?? "操作"}失败：${errorMessage(error)}`, "error");
  } finally {
    if (sequence === resultSequence) {
      busy = completed || closed;
      if (busy) {
        for (const control of actionButtons) {
          if (control.dataset.resultAction !== "close") control.disabled = true;
        }
      }
    }
  }
}

const actionButtons = [...document.querySelectorAll<HTMLButtonElement>("[data-result-action]")];
for (const button of actionButtons) {
  button.addEventListener("click", () =>
    void runAction(button.dataset.resultAction ?? "", button),
  );
}
document.getElementById("resultCloseIcon")?.addEventListener("click", () => dismiss());

document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  dismiss();
});

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

onScrollResult((result) => applyResult(normalizeResult(result as unknown as Record<string, unknown>)));

const params = new URLSearchParams(window.location.search);
const fromQuery = queryResult(params);
if (!hasNative || params.get("demo") === "1") {
  applyResult(fromQuery ?? demoResult(params));
} else if (fromQuery) {
  applyResult(fromQuery);
} else {
  // Native: Go emits `scroll:result` right after showing the window. Keep the
  // spinner if it is slow; the event will fill the page in.
  window.setTimeout(() => {
    if (!loaded) metaEl.textContent = "正在等待长图…";
  }, 2500);
}
