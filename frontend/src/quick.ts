import { dismissQuickCapture, hasNative, quickAction, quickCapture, showQuickCapture } from "./bridge";

const closeDelay = 6000;
const card = document.getElementById("quickCard")!;
const thumbnail = document.getElementById("quickThumbnail") as HTMLImageElement;
const summary = document.getElementById("quickSummary")!;
const status = document.getElementById("quickStatus")!;
const closeButton = document.getElementById("quickClose") as HTMLButtonElement;
const actions = [...card.querySelectorAll<HTMLButtonElement>("[data-quick-action]")];
let id = "";
let hovered = false;
let busy = false;
let closing = false;
let timer: number | undefined;

function scheduleDismiss(): void {
  if (timer !== undefined) window.clearTimeout(timer);
  timer = undefined;
  if (id && !hovered && !busy && !closing) timer = window.setTimeout(() => void dismiss(), closeDelay);
}

function setBusy(pending: boolean): void {
  busy = pending;
  for (const button of [closeButton, ...actions]) button.disabled = pending;
  scheduleDismiss();
}

function feedback(message = "", isError = false): void {
  status.textContent = message;
  status.title = message;
  if (isError) status.setAttribute("data-error", "");
  else status.removeAttribute("data-error");
}

async function dismiss(): Promise<void> {
  if (!id || busy || closing) return;
  closing = true;
  setBusy(true);
  try {
    await dismissQuickCapture(id);
    card.hidden = true;
  } catch (error) {
    closing = false;
    feedback(error instanceof Error ? error.message : "无法关闭截图快捷操作", true);
    setBusy(false);
  }
}

async function run(action: string): Promise<void> {
  if (!id || busy || closing) return;
  setBusy(true);
  feedback("处理中…");
  try {
    const result = await quickAction(id, action);
    if (!result.completed) {
      feedback(result.cancelled ? "已取消保存" : "请重试", !result.cancelled);
      return;
    }
    setBusy(false);
    await dismiss();
  } catch (error) {
    feedback(error instanceof Error ? error.message : "快捷操作失败，请重试", true);
  } finally {
    if (!closing) setBusy(false);
  }
}

card.addEventListener("pointerenter", () => {
  hovered = true;
  scheduleDismiss();
});
card.addEventListener("pointerleave", () => {
  hovered = false;
  scheduleDismiss();
});
closeButton.addEventListener("click", () => void dismiss());
window.addEventListener("keydown", (event) => {
  if (event.key === "Escape") void dismiss();
});
for (const button of actions) button.addEventListener("click", () => void run(button.dataset.quickAction ?? ""));

async function loadCapture(): Promise<void> {
  const capture = await quickCapture();
  id = capture.id;
  thumbnail.src = capture.thumbnail;
  const completion = capture.completion === "save" ? "已保存" : "已复制";
  summary.textContent = `${completion} · ${capture.width} × ${capture.height}`;
  await thumbnail.decode?.().catch(() => {});
  card.hidden = false;
  await showQuickCapture(id);
  scheduleDismiss();
}

if (hasNative) void loadCapture().catch(() => {});
