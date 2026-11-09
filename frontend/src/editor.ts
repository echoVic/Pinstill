// The capture editor: draws the real screenshot as the canvas base and keeps
// the design prototype's annotation tools, properties bar, result panel and
// status line working on top of it.
//
// The canvas is never tainted: the screenshot arrives as an ImageBitmap or an
// <img> decoded from a Blob (see util.loadCanvasImage), so mosaic sampling and
// PNG/JPEG export keep working.

import { drawDemoScene } from "./demo";
import { refreshTupenIcons } from "./icons";
import type { Preferences } from "./settings";
import { formatAccelerator } from "./settings";
import { isDrawTool, TOOLS, TOOL_ICONS, type ToolId } from "./tools";
import { blobToBase64 } from "./util";

interface Point {
  x: number;
  y: number;
}

interface Shape {
  kind: string;
  x: number;
  y: number;
  x2: number;
  y2: number;
  color: string;
  width: number;
  dx?: number;
  dy?: number;
  text?: string;
  points?: Point[];
  _origin?: Point;
}

type EditorMode = "empty" | "demo" | "image";

/** Actions the editor delegates to the page (native calls live there). */
export interface EditorActions {
  pin: () => void | Promise<void>;
  translate: () => void;
  ocr: () => void;
  qr: () => void;
  scroll: () => void;
  download: () => void | Promise<void>;
  clipboard: () => void | Promise<void>;
}

export interface EditorOptions {
  canvas: HTMLCanvasElement;
  toolbar: HTMLElement;
  properties: HTMLElement;
  resultPanel: HTMLElement;
  status: HTMLElement;
  captureSize: HTMLElement;
  captureHint: HTMLElement;
  getPreferences: () => Preferences;
  actions: EditorActions;
}

interface ResultAction {
  label: string;
  onClick: () => void;
}

const BASE_WIDTH = 960;
const BASE_HEIGHT = 480;

function roundRectPath(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  width: number,
  height: number,
  radius: number,
): void {
  if (typeof ctx.roundRect === "function") {
    ctx.beginPath();
    ctx.roundRect(x, y, width, height, radius);
    return;
  }
  const r = Math.min(radius, width / 2, height / 2);
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.arcTo(x + width, y, x + width, y + height, r);
  ctx.arcTo(x + width, y + height, x, y + height, r);
  ctx.arcTo(x, y + height, x, y, r);
  ctx.arcTo(x, y, x + width, y, r);
  ctx.closePath();
}

export class Editor {
  readonly canvas: HTMLCanvasElement;

  private readonly ctx: CanvasRenderingContext2D;
  private readonly toolbar: HTMLElement;
  private readonly propertiesRoot: HTMLElement;
  private readonly resultPanel: HTMLElement;
  private readonly statusEl: HTMLElement;
  private readonly captureSizeEl: HTMLElement;
  private readonly captureHintEl: HTMLElement;
  private readonly getPreferences: () => Preferences;
  private readonly actions: EditorActions;

  private mode: EditorMode = "empty";
  private image: ImageBitmap | HTMLImageElement | null = null;
  private current: ToolId = "arrow";
  private color = "#e56e48";
  private width = 4;
  private entry = "重点说明";
  private shapes: Shape[] = [];
  private draft: Shape | null = null;
  private start: Point | null = null;
  private dragged: Shape | null = null;
  private labelNumber = 1;
  private scale = 1;

  constructor(options: EditorOptions) {
    this.canvas = options.canvas;
    const ctx = this.canvas.getContext("2d");
    if (!ctx) throw new Error("2D canvas is not available");
    this.ctx = ctx;
    this.toolbar = options.toolbar;
    this.propertiesRoot = options.properties;
    this.resultPanel = options.resultPanel;
    this.statusEl = options.status;
    this.captureSizeEl = options.captureSize;
    this.captureHintEl = options.captureHint;
    this.getPreferences = options.getPreferences;
    this.actions = options.actions;

    this.buildToolbar();
    this.bindCanvasEvents();
    this.drawEmptyState("等待截图");
  }

  // ---------------------------------------------------------------- capture

  /** Replaces the canvas base with a real capture and resets annotations. */
  setBase(image: ImageBitmap | HTMLImageElement, meta?: { width?: number; height?: number }): void {
    this.image = image;
    const width = meta?.width && meta.width > 0 ? meta.width : image.width;
    const height = meta?.height && meta.height > 0 ? meta.height : image.height;
    this.canvas.width = Math.max(1, Math.round(width));
    this.canvas.height = Math.max(1, Math.round(height));
    this.scale = this.canvas.width / BASE_WIDTH;
    this.mode = "image";
    this.shapes = [];
    this.draft = null;
    this.dragged = null;
    this.labelNumber = 1;
    this.applyDefaultStyle();
    this.showToolbar();
    this.hideResult();
    this.setTool(this.getPreferences().defaultTool);
    this.redraw();
    this.captureHintEl.textContent = "截取完成 · 可继续标注";
    this.notify("截取完成 · 选择工具，在截图上继续标注");
  }

  /** Browser preview only: the design's demo screenshot. */
  setDemo(presetShapes = true): void {
    this.image = null;
    this.canvas.width = BASE_WIDTH;
    this.canvas.height = BASE_HEIGHT;
    this.scale = 1;
    this.mode = "demo";
    this.shapes = presetShapes
      ? [
          { kind: "rect", x: 199, y: 93, x2: 491, y2: 170, color: "#e56e48", width: 3 },
          { kind: "arrow", x: 560, y: 205, x2: 476, y2: 146, color: "#e56e48", width: 4 },
          { kind: "tag", x: 887, y: 291, x2: 887, y2: 291, color: "#e56e48", width: 2, text: "1" },
        ]
      : [];
    this.draft = null;
    this.dragged = null;
    this.labelNumber = 2;
    this.applyDefaultStyle();
    this.showToolbar();
    this.hideResult();
    this.setTool(this.getPreferences().defaultTool);
    this.redraw();
    this.captureHintEl.textContent = "浏览器预览 · 演示截图";
    this.notify("浏览器预览 · 可切换工具，在截图上拖动试画");
  }

  /** Clears the editor back to the empty state (desktop app, before a capture). */
  setEmpty(): void {
    this.image = null;
    this.canvas.width = BASE_WIDTH;
    this.canvas.height = BASE_HEIGHT;
    this.scale = 1;
    this.mode = "empty";
    this.shapes = [];
    this.draft = null;
    this.dragged = null;
    this.applyDefaultStyle();
    this.showToolbar();
    this.hideResult();
    this.captureHintEl.textContent = "等待截图 · 点击右上角「截一张」";
    this.notify(
      `点击右上角「截一张」，或按 ${formatAccelerator(this.getPreferences().captureKey)} 开始截取`,
    );
    this.redraw();
  }

  hasCapture(): boolean {
    return this.mode !== "empty";
  }

  getCanvas(): HTMLCanvasElement {
    return this.canvas;
  }

  imageInfo(): { width: number; height: number } | null {
    if (this.mode === "empty") return null;
    return { width: this.canvas.width, height: this.canvas.height };
  }

  // ------------------------------------------------------------------ tools

  setTool(id: string): void {
    if (!isDrawTool(id)) return;
    this.current = id;
    if (id === "watermark") this.entry = this.getPreferences().watermarkText;
    this.toolbar.querySelectorAll<HTMLButtonElement>("[aria-pressed]").forEach((button) => {
      const active = button.dataset.tool === id;
      button.classList.toggle("active", active);
      button.setAttribute("aria-pressed", String(active));
    });
    this.canvas.style.cursor = id === "drag" ? "move" : "crosshair";
    this.renderProperties();
  }

  undo(): void {
    if (this.shapes.length === 0) {
      this.notify("没有可撤销的标注");
      return;
    }
    this.shapes.pop();
    this.redraw();
    this.notify("已撤销最近一处标注");
  }

  /** The toolbar's "取消": leave annotation mode without touching the capture. */
  exitCapture(): void {
    this.toolbar.classList.add("hidden");
    this.propertiesRoot.hidden = true;
    this.hideResult();
    this.notify("已退出本次截图 · 点击“截一张”重新进入");
  }

  // ----------------------------------------------------------------- render

  redraw(): void {
    const { width, height } = this.canvas;
    this.ctx.clearRect(0, 0, width, height);
    this.renderBase();
    for (const shape of this.shapes) this.drawShape(shape);
    if (this.draft) this.drawShape(this.draft);
    const undoButton = this.toolbar.querySelector<HTMLButtonElement>('[data-tool="undo"]');
    if (undoButton) undoButton.disabled = this.shapes.length === 0;
    this.updateSizeLabel();
  }

  private updateSizeLabel(): void {
    if (this.mode === "empty") {
      this.captureSizeEl.textContent = "尚未截取";
      return;
    }
    this.captureSizeEl.textContent = `${this.canvas.width} × ${this.canvas.height} px`;
  }

  private renderBase(): void {
    if (this.mode === "image" && this.image) {
      this.ctx.drawImage(this.image, 0, 0, this.canvas.width, this.canvas.height);
      return;
    }
    if (this.mode === "demo") {
      drawDemoScene(this.ctx, this.canvas.width, this.canvas.height);
      return;
    }
    this.drawEmptyState();
  }

  private drawEmptyState(label = "等待截图"): void {
    const { width, height } = this.canvas;
    this.ctx.fillStyle = "#f4f1eb";
    this.ctx.fillRect(0, 0, width, height);
    this.ctx.strokeStyle = "#d6d0c3";
    this.ctx.lineWidth = 2;
    this.ctx.setLineDash([9, 7]);
    roundRectPath(this.ctx, 28, 28, Math.max(1, width - 56), Math.max(1, height - 56), 14);
    this.ctx.stroke();
    this.ctx.setLineDash([]);
    this.ctx.textAlign = "center";
    this.ctx.fillStyle = "#6f6a5e";
    this.ctx.font = "600 24px -apple-system, sans-serif";
    this.ctx.fillText(label, width / 2, height / 2 - 6);
    this.ctx.font = "14px -apple-system, sans-serif";
    this.ctx.fillStyle = "#908a7c";
    this.ctx.fillText("点击右上角「截一张」，或按快捷键开始截取", width / 2, height / 2 + 26);
    this.ctx.textAlign = "start";
  }

  // ----------------------------------------------------------------- shapes

  private drawShape(shape: Shape): void {
    const ctx = this.ctx;
    const scale = this.scale;
    const lineWidth = Math.max(1, shape.width * scale);
    ctx.save();
    ctx.translate(shape.dx ?? 0, shape.dy ?? 0);
    ctx.strokeStyle = shape.color;
    ctx.fillStyle = shape.color;
    ctx.lineWidth = lineWidth;
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    const x = Math.min(shape.x, shape.x2);
    const y = Math.min(shape.y, shape.y2);
    const w = Math.abs(shape.x2 - shape.x);
    const h = Math.abs(shape.y2 - shape.y);

    if (shape.kind === "rect") ctx.strokeRect(x, y, w, h);
    if (shape.kind === "ellipse") {
      ctx.beginPath();
      ctx.ellipse(
        x + w / 2,
        y + h / 2,
        Math.max(w / 2, 1),
        Math.max(h / 2, 1),
        0,
        0,
        Math.PI * 2,
      );
      ctx.stroke();
    }
    if (shape.kind === "line" || shape.kind === "arrow") {
      this.strokePath([
        { x: shape.x, y: shape.y },
        { x: shape.x2, y: shape.y2 },
      ]);
      if (shape.kind === "arrow") {
        const angle = Math.atan2(shape.y2 - shape.y, shape.x2 - shape.x);
        const length = (16 + shape.width) * scale;
        this.strokePath([
          {
            x: shape.x2 - length * Math.cos(angle - 0.45),
            y: shape.y2 - length * Math.sin(angle - 0.45),
          },
          { x: shape.x2, y: shape.y2 },
          {
            x: shape.x2 - length * Math.cos(angle + 0.45),
            y: shape.y2 - length * Math.sin(angle + 0.45),
          },
        ]);
      }
    }
    if (shape.kind === "brush" && shape.points) this.strokePath(shape.points);
    if (shape.kind === "highlight") {
      ctx.globalAlpha = 0.32;
      ctx.fillRect(x, y, w, h);
      ctx.globalAlpha = 1;
    }
    if (shape.kind === "text") {
      ctx.font = `500 ${(shape.width * 3 + 16) * scale}px -apple-system, sans-serif`;
      ctx.fillText(shape.text ?? "", shape.x, shape.y);
    }
    if (shape.kind === "tag") {
      ctx.beginPath();
      ctx.arc(shape.x, shape.y, (17 + shape.width) * scale, 0, Math.PI * 2);
      ctx.fill();
      ctx.font = `bold ${20 * scale}px sans-serif`;
      ctx.textAlign = "center";
      ctx.fillStyle = "#fff8ed";
      ctx.fillText(shape.text ?? "", shape.x, shape.y + 7 * scale);
      ctx.textAlign = "start";
    }
    if (shape.kind === "watermark") {
      ctx.globalAlpha = this.getPreferences().watermarkOpacity / 100;
      ctx.font = `bold ${(shape.width * 3 + 18) * scale}px -apple-system, sans-serif`;
      ctx.fillText(shape.text ?? "", shape.x, shape.y);
      ctx.globalAlpha = 1;
    }
    if (shape.kind === "mosaic") this.drawMosaic(shape, x, y, w, h);
    ctx.restore();
  }

  private strokePath(points: Point[]): void {
    this.ctx.beginPath();
    points.forEach((point, index) => {
      if (index === 0) this.ctx.moveTo(point.x, point.y);
      else this.ctx.lineTo(point.x, point.y);
    });
    this.ctx.stroke();
  }

  private drawMosaic(shape: Shape, x: number, y: number, w: number, h: number): void {
    const ctx = this.ctx;
    const cell = Math.max(2, Math.round((shape.width * 2 + 4) * this.scale));
    const columns = Math.ceil(w / cell);
    const rows = Math.ceil(h / cell);
    const isPreview = shape === this.draft;
    if (isPreview && columns * rows > 24_000) {
      // Cheap approximation while dragging a huge region; the committed shape
      // gets the full pixelated render.
      ctx.globalAlpha = 0.18;
      ctx.fillStyle = "#3d3a34";
      ctx.fillRect(x, y, w, h);
      ctx.globalAlpha = 1;
      return;
    }
    const left = Math.max(0, Math.floor(x));
    const top = Math.max(0, Math.floor(y));
    const right = Math.min(this.canvas.width, Math.ceil(x + w));
    const bottom = Math.min(this.canvas.height, Math.ceil(y + h));
    if (right <= left || bottom <= top) return;

    const sample = document.createElement("canvas");
    sample.width = right - left;
    sample.height = bottom - top;
    const sampleCtx = sample.getContext("2d", { willReadFrequently: true });
    if (!sampleCtx) return;
    sampleCtx.drawImage(
      this.canvas,
      left,
      top,
      sample.width,
      sample.height,
      0,
      0,
      sample.width,
      sample.height,
    );

    ctx.save();
    ctx.beginPath();
    ctx.rect(x, y, w, h);
    ctx.clip();
    for (let yy = y; yy < y + h; yy += cell) {
      for (let xx = x; xx < x + w; xx += cell) {
        const px = Math.min(sample.width - 1, Math.max(0, Math.floor(xx + cell / 2) - left));
        const py = Math.min(sample.height - 1, Math.max(0, Math.floor(yy + cell / 2) - top));
        const data = sampleCtx.getImageData(px, py, 1, 1).data;
        ctx.fillStyle = `rgb(${data[0]},${data[1]},${data[2]})`;
        ctx.fillRect(xx, yy, cell, cell);
      }
    }
    ctx.restore();
  }

  // --------------------------------------------------------------- pointer

  private bindCanvasEvents(): void {
    this.canvas.addEventListener("pointerdown", (event) => this.onPointerDown(event));
    this.canvas.addEventListener("pointermove", (event) => this.onPointerMove(event));
    const finish = () => this.finishShape();
    this.canvas.addEventListener("pointerup", finish);
    this.canvas.addEventListener("pointercancel", finish);
  }

  private coords(event: PointerEvent): Point {
    const rect = this.canvas.getBoundingClientRect();
    return {
      x: ((event.clientX - rect.left) * this.canvas.width) / rect.width,
      y: ((event.clientY - rect.top) * this.canvas.height) / rect.height,
    };
  }

  private onPointerDown(event: PointerEvent): void {
    if (event.button !== 0) return;
    if (this.toolbar.classList.contains("hidden")) return;
    if (this.mode === "empty") {
      this.notify("先截取一张图片，再开始标注");
      return;
    }
    event.preventDefault();
    const point = this.coords(event);
    this.canvas.setPointerCapture(event.pointerId);
    this.start = point;
    if (this.current === "drag") {
      this.dragged = this.shapes.at(-1) ?? null;
      if (this.dragged) {
        this.dragged._origin = { x: this.dragged.dx ?? 0, y: this.dragged.dy ?? 0 };
      }
      return;
    }
    this.draft = {
      kind: this.current,
      x: point.x,
      y: point.y,
      x2: point.x,
      y2: point.y,
      color: this.color,
      width: this.width,
      points: [point],
      text: this.current === "tag" ? String(this.labelNumber++) : this.entry,
    };
    if (this.current === "text" || this.current === "tag" || this.current === "watermark") {
      this.shapes.push(this.draft);
      this.draft = null;
      this.start = null;
      this.redraw();
    }
  }

  private onPointerMove(event: PointerEvent): void {
    if (!this.start) return;
    const point = this.coords(event);
    if (this.dragged) {
      this.dragged.dx = (this.dragged._origin?.x ?? 0) + point.x - this.start.x;
      this.dragged.dy = (this.dragged._origin?.y ?? 0) + point.y - this.start.y;
      this.redraw();
      return;
    }
    if (!this.draft) return;
    this.draft.x2 = point.x;
    this.draft.y2 = point.y;
    if (this.draft.kind === "brush" && this.draft.points) this.draft.points.push(point);
    this.redraw();
  }

  private finishShape(): void {
    if (this.draft) {
      const draft = this.draft;
      if (Math.abs(draft.x2 - draft.x) + Math.abs(draft.y2 - draft.y) > 3) {
        this.shapes.push(draft);
      }
      this.draft = null;
    }
    this.start = null;
    this.dragged = null;
    this.redraw();
  }

  // ------------------------------------------------------------- properties

  private renderProperties(): void {
    const prefs = this.getPreferences();
    const root = this.propertiesRoot;
    root.replaceChildren();
    const title = document.createElement("span");
    title.className = "property-name";
    title.textContent = TOOLS.find(([id]) => id === this.current)?.[1] ?? "";
    root.append(title);
    if (this.current === "drag") {
      const hint = document.createElement("span");
      hint.textContent = "拖动最近一处标注，调整位置";
      root.append(hint);
      return;
    }
    ["#e56e48", "#efb94c", "#78a576", "#70a8c2", "#f6eee1"].forEach((swatch) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `color-swatch${this.color === swatch ? " selected" : ""}`;
      button.style.setProperty("--swatch", swatch);
      button.setAttribute("aria-label", `颜色 ${swatch}`);
      button.addEventListener("click", () => {
        this.color = swatch;
        this.renderProperties();
      });
      root.append(button);
    });
    const separator = document.createElement("span");
    separator.className = "property-sep";
    root.append(separator);
    const label = document.createElement("label");
    label.textContent = this.current === "text" || this.current === "watermark" ? "字号" : "粗细";
    const range = document.createElement("input");
    range.type = "range";
    range.min = "1";
    range.max = "10";
    range.value = String(this.width);
    range.setAttribute("aria-label", label.textContent);
    const value = document.createElement("span");
    value.textContent = String(this.width);
    range.addEventListener("input", () => {
      this.width = Number(range.value);
      value.textContent = range.value;
    });
    label.append(range, value);
    root.append(label);
    if (this.current === "text" || this.current === "watermark" || this.current === "tag") {
      const input = document.createElement("input");
      input.type = "text";
      input.value =
        this.current === "tag" ? String(this.labelNumber) : this.entry;
      input.placeholder = "输入内容";
      input.setAttribute("aria-label", "标注内容");
      input.addEventListener("input", () => {
        this.entry = input.value;
      });
      root.append(input);
    }
    const hint = document.createElement("span");
    hint.className = "tool-hint";
    hint.textContent =
      this.current === "text" || this.current === "tag" || this.current === "watermark"
        ? "点击截图放置"
        : "在截图上拖动绘制";
    root.append(hint);
    if (this.current === "watermark") {
      // Keep the persisted watermark text when the tool is (re)selected.
      this.entry = prefs.watermarkText;
    }
  }

  // ------------------------------------------------------------------ chrome

  private buildToolbar(): void {
    this.toolbar.replaceChildren();
    const groups = [TOOLS.slice(0, 11), TOOLS.slice(11)];
    groups.forEach((group, groupIndex) => {
      const row = document.createElement("div");
      row.className = "toolbar-group";
      row.setAttribute("role", "group");
      row.setAttribute("aria-label", groupIndex ? "识别与完成" : "绘制与标注");
      group.forEach(([id, name]) => {
        const button = document.createElement("button");
        button.type = "button";
        button.dataset.tool = id;
        button.setAttribute("aria-label", name);
        if (groupIndex === 0) button.setAttribute("aria-pressed", "false");
        button.innerHTML = `<i data-lucide="${TOOL_ICONS[id]}" aria-hidden="true"></i><span aria-hidden="true">${name}</span>`;
        if (id === "clipboard") button.className = "copy-action";
        if (id === "cancel") button.className = "cancel-action";
        button.addEventListener("click", () => this.activate(id));
        row.append(button);
      });
      this.toolbar.append(row);
    });
    refreshTupenIcons();
    const undoButton = this.toolbar.querySelector<HTMLButtonElement>('[data-tool="undo"]');
    if (undoButton) undoButton.disabled = true;
  }

  private activate(id: ToolId): void {
    this.hideResult();
    if (isDrawTool(id)) {
      this.setTool(id);
      return;
    }
    switch (id) {
      case "undo":
        this.undo();
        return;
      case "cancel":
        this.exitCapture();
        return;
      case "pin":
        void this.actions.pin();
        return;
      case "translate":
        this.actions.translate();
        return;
      case "ocr":
        this.actions.ocr();
        return;
      case "qr":
        this.actions.qr();
        return;
      case "scroll":
        this.actions.scroll();
        return;
      case "download":
        void this.actions.download();
        return;
      case "clipboard":
        void this.actions.clipboard();
        return;
      default:
        return;
    }
  }

  private showToolbar(): void {
    this.toolbar.classList.remove("hidden");
    this.propertiesRoot.hidden = false;
  }

  private applyDefaultStyle(): void {
    const prefs = this.getPreferences();
    this.color = prefs.defaultColor;
    this.width = prefs.defaultWidth;
    this.entry = prefs.watermarkText;
  }

  // -------------------------------------------------------------- messages

  notify(text: string): void {
    this.statusEl.textContent = text;
  }

  showResult(title: string, text: string, action?: ResultAction): void {
    const panel = this.resultPanel;
    panel.replaceChildren();
    const heading = document.createElement("strong");
    heading.textContent = title;
    const body = document.createElement("div");
    body.textContent = text;
    panel.append(heading, body);
    if (action) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "result-action";
      button.textContent = action.label;
      button.addEventListener("click", action.onClick);
      panel.append(button);
    }
    panel.hidden = false;
  }

  hideResult(): void {
    this.resultPanel.hidden = true;
  }

  // ---------------------------------------------------------------- export

  /** Encodes the current canvas (base + annotations) as plain base64. */
  async toBase64(type = "image/png", quality?: number): Promise<string> {
    const blob = await new Promise<Blob>((resolve, reject) => {
      this.canvas.toBlob(
        (result) => {
          if (result) resolve(result);
          else reject(new Error("画布编码失败"));
        },
        type,
        quality,
      );
    });
    return blobToBase64(blob);
  }

  /** PNG base64 — required by `Capture.CopyImage` and `Capture.PinImage`. */
  toPNGBase64(): Promise<string> {
    return this.toBase64("image/png");
  }
}
