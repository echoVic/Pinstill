import {
  hangClipboard,
  hideClothesline,
  importClotheslineImage,
  listClothesline,
  moveClotheslineImage,
  onLineChanged,
  removeClotheslineImage,
  useClotheslineImage,
  type LineView,
} from "./bridge";

const photos = document.getElementById("linePhotos")!;
const empty = document.getElementById("lineEmpty")!;
const controls = document.getElementById("lineControls")!;
const status = document.getElementById("lineStatus")!;
const moveLeft = document.getElementById("lineMoveLeft") as HTMLButtonElement;
const moveRight = document.getElementById("lineMoveRight") as HTMLButtonElement;

let view: LineView = { revision: -1, entries: [] };
let selected = "";
let dragging = "";
let busy = false;

function setStatus(text: string, kind = "info"): void {
  status.textContent = text;
  status.dataset.kind = kind;
}

function updateSelection(): void {
  const index = view.entries.findIndex((entry) => entry.id === selected);
  for (const card of photos.querySelectorAll<HTMLButtonElement>("[data-line-id]")) {
    card.setAttribute("aria-pressed", String(card.dataset.lineId === selected));
  }
  controls.hidden = index < 0;
  for (const button of document.querySelectorAll<HTMLButtonElement>("header button, #lineControls button")) {
    button.disabled = busy;
  }
  moveLeft.disabled = busy || index <= 0;
  moveRight.disabled = busy || index < 0 || index >= view.entries.length - 1;
}

function applyView(next: LineView): void {
  if (next.revision <= view.revision) return;
  const focusedPhoto = document.activeElement instanceof HTMLButtonElement ? document.activeElement.dataset.lineId : undefined;
  view = next;
  if (!next.entries.some((entry) => entry.id === selected)) selected = "";
  empty.textContent = "用「选图」或从截图的「晾图」把图片挂上来。";
  empty.hidden = next.entries.length > 0;
  photos.hidden = next.entries.length === 0;
  photos.replaceChildren(...next.entries.map((entry) => {
    const card = document.createElement("button");
    card.type = "button";
    card.className = "line-photo";
    card.dataset.lineId = entry.id;
    card.title = entry.name;
    card.draggable = true;
    const image = document.createElement("img");
    image.src = entry.thumbUrl;
    image.alt = "";
    image.draggable = false;
    const title = document.createElement("span");
    title.textContent = entry.name;
    card.append(image, title);
    card.addEventListener("click", () => {
      if (busy) return;
      selected = entry.id;
      setStatus("");
      updateSelection();
    });
    card.addEventListener("dragstart", (event) => {
      if (busy) { event.preventDefault(); return; }
      dragging = entry.id;
      selected = entry.id;
      event.dataTransfer?.setData("text/plain", entry.id);
      updateSelection();
    });
    card.addEventListener("dragend", () => { dragging = ""; });
    card.addEventListener("dragover", (event) => {
      if (!dragging || dragging === entry.id) return;
      event.preventDefault();
      card.classList.add("drop-target");
    });
    card.addEventListener("dragleave", () => card.classList.remove("drop-target"));
    card.addEventListener("drop", (event) => {
      if (!dragging || dragging === entry.id) return;
      event.preventDefault();
      card.classList.remove("drop-target");
      const source = dragging;
      dragging = "";
      const bounds = card.getBoundingClientRect();
      const after = event.clientX >= bounds.left + bounds.width / 2;
      const index = view.entries.findIndex((current) => current.id === entry.id);
      const before = after ? view.entries[index + 1]?.id ?? "" : entry.id;
      void perform(() => moveClotheslineImage(source, before));
    });
    return card;
  }));
  updateSelection();
  if (focusedPhoto) {
    const replacement = Array.from(photos.querySelectorAll<HTMLButtonElement>("[data-line-id]"))
      .find((card) => card.dataset.lineId === focusedPhoto);
    (replacement ?? document.getElementById("lineImport"))?.focus();
  }
}

async function perform(action: () => Promise<unknown>, message = ""): Promise<void> {
  if (busy) return;
  busy = true;
  updateSelection();
  setStatus("");
  try {
    if (await action() !== false) setStatus(message);
  } catch (error) {
    setStatus(error instanceof Error ? error.message : "晾绳操作失败", "error");
  } finally {
    busy = false;
    updateSelection();
  }
}

function shiftSelection(delta: number): void {
  const index = view.entries.findIndex((entry) => entry.id === selected);
  if (index < 0 || !view.entries[index + delta]) return;
  const before = view.entries[index + (delta < 0 ? -1 : 2)]?.id ?? "";
  const id = selected;
  void perform(() => moveClotheslineImage(id, before));
}

function useSelected(action: "copy" | "pin" | "save", message: string): void {
  const id = selected;
  if (id) void perform(() => useClotheslineImage(id, action), message);
}

moveLeft.addEventListener("click", () => shiftSelection(-1));
moveRight.addEventListener("click", () => shiftSelection(1));
document.getElementById("lineCopy")!.addEventListener("click", () => useSelected("copy", "已复制"));
document.getElementById("lineSave")!.addEventListener("click", () => useSelected("save", "已保存"));
document.getElementById("linePin")!.addEventListener("click", () => useSelected("pin", "已创建置顶贴图"));
document.getElementById("lineRemove")!.addEventListener("click", () => {
  const id = selected;
  if (id) void perform(() => removeClotheslineImage(id), "已取下");
});
document.getElementById("lineImport")!.addEventListener("click", () => void perform(importClotheslineImage));
document.getElementById("lineClipboard")!.addEventListener("click", () => void perform(hangClipboard));
document.getElementById("lineHide")!.addEventListener("click", () => void perform(hideClothesline));
document.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  event.preventDefault();
  void perform(hideClothesline);
});

onLineChanged(applyView);
void listClothesline().then(applyView).catch((error: unknown) => {
  if (view.revision >= 0) return;
  empty.textContent = "无法加载晾绳";
  setStatus(error instanceof Error ? error.message : "无法读取晾绳记录", "error");
});
