// The capture editor: draws the real screenshot as the canvas base and keeps
// the design prototype's annotation tools, properties bar, result panel and
// status line working on top of it.
//
// The canvas is never tainted: the screenshot arrives as an ImageBitmap or an
// <img> decoded from a Blob (see util.loadCanvasImage), so mosaic sampling and
// PNG/JPEG export keep working.

import {
  drawShape,
  InlineTextEditor,
  roundRectPath,
  type Point,
  type Shape,
  type ShapeRenderStyle,
} from "./annotate";
import {
  isPresetColor,
  loadRecentColors,
  normalizeColor,
  PRESET_COLORS,
  rememberColor,
} from "./colors";
import { drawDemoScene } from "./demo";
import { refreshPinstillIcons } from "./icons";
import type { Preferences } from "./settings";
import { formatAccelerator } from "./settings";
import { isDrawTool, TOOLS, TOOL_ICONS, type ToolId } from "./tools";
import { blobToBase64 } from "./util";

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
  private scale = 1;
  private inlineEditor: InlineTextEditor | null = null;
  private recentColors: string[] = loadRecentColors();
  private readonly customColorInput: HTMLInputElement | null;

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
    this.customColorInput = document.getElementById("customColor") as HTMLInputElement | null;
    if (this.customColorInput) {
      this.customColorInput.addEventListener("input", () => {
        if (this.customColorInput) this.setColor(this.customColorInput.value);
      });
      this.customColorInput.addEventListener("change", () => {
        if (this.customColorInput) this.setColor(this.customColorInput.value, true);
      });
    }

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
    this.cancelInlineEditor();
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
    this.cancelInlineEditor();
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
    this.cancelInlineEditor();
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
    this.canvas.style.cursor = id === "drag" ? "move" : id === "text" ? "text" : "crosshair";
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
    this.cancelInlineEditor();
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
    for (const shape of this.shapes) drawShape(this.ctx, shape, this.shapeStyle(shape));
    if (this.draft) drawShape(this.ctx, this.draft, this.shapeStyle(this.draft));
    const undoButton = this.toolbar.querySelector<HTMLButtonElement>('[data-tool="undo"]');
    if (undoButton) undoButton.disabled = this.shapes.length === 0;
    this.updateSizeLabel();
  }

  private updateSizeLabel(): void {
    if (this.mode === "empty") {
      this.captureSizeEl.textContent = "尚未截取";
      this.captureSizeEl.removeAttribute("title");
      return;
    }
    const size = `${this.canvas.width} × ${this.canvas.height} px`;
    // R7.3: the canvas is fitted to the frame, so show both the fit state and
    // the real pixel size (the editing geometry stays untouched).
    this.captureSizeEl.textContent = `适应窗口 · 实际 ${size}`;
    this.captureSizeEl.title = `实际尺寸 ${size}`;
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

  private shapeStyle(shape: Shape): ShapeRenderStyle {
    return {
      scale: this.scale,
      watermarkOpacity: this.getPreferences().watermarkOpacity,
      canvas: this.canvas,
      preview: shape === this.draft,
    };
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
    if (this.current === "text" || this.current === "tag") {
      this.openInlineText(point);
      return;
    }
    try {
      this.canvas.setPointerCapture(event.pointerId);
    } catch {
      // Synthetic events (automation) have no capturable pointer; canvas
      // listeners still receive the moves.
    }
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
      text: this.entry,
    };
    if (this.current === "watermark") {
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
    this.renderColorPalette(root);
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
    if (this.current === "watermark") {
      const input = document.createElement("input");
      input.type = "text";
      input.value = this.entry;
      input.placeholder = "输入水印文字";
      input.setAttribute("aria-label", "标注内容");
      input.addEventListener("input", () => {
        this.entry = input.value;
      });
      root.append(input);
    }
    const hint = document.createElement("span");
    hint.className = "tool-hint";
    if (this.current === "text" || this.current === "tag") {
      hint.textContent = "点击画布输入文字";
    } else if (this.current === "watermark") {
      hint.textContent = "点击截图放置";
    } else {
      hint.textContent = "在截图上拖动绘制";
    }
    root.append(hint);
    if (this.current === "watermark") {
      // Keep the persisted watermark text when the tool is (re)selected.
      this.entry = prefs.watermarkText;
    }
  }

  private renderColorPalette(root: HTMLElement): void {
    const palette = document.createElement("span");
    palette.className = "color-palette";
    const current = this.color.toLowerCase();
    const custom = !isPresetColor(current) && !this.recentColors.includes(current);

    for (const preset of PRESET_COLORS) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `color-swatch${preset.toLowerCase() === current ? " selected" : ""}`;
      button.dataset.color = preset;
      button.style.setProperty("--swatch", preset);
      button.setAttribute("aria-label", `颜色 ${preset}`);
      button.setAttribute("aria-pressed", String(preset.toLowerCase() === current));
      button.addEventListener("click", () => this.setColor(preset));
      palette.append(button);
    }

    const customButton = document.createElement("button");
    customButton.type = "button";
    customButton.className = `color-swatch custom${custom ? " selected" : ""}`;
    customButton.dataset.colorCustom = "true";
    customButton.setAttribute("aria-label", "自定义颜色");
    customButton.setAttribute("aria-pressed", String(custom));
    customButton.title = "自定义颜色";
    customButton.addEventListener("click", () => this.customColorInput?.click());
    palette.append(customButton);

    for (const recent of this.recentColors) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = `color-swatch${recent === current ? " selected" : ""}`;
      button.dataset.colorRecent = recent;
      button.style.setProperty("--swatch", recent);
      button.setAttribute("aria-label", `最近使用颜色 ${recent}`);
      button.setAttribute("aria-pressed", String(recent === current));
      button.addEventListener("click", () => this.setColor(recent));
      palette.append(button);
    }
    root.append(palette);
  }

  private setColor(next: string, remember = false): void {
    const normalized = normalizeColor(next);
    if (!normalized) return;
    this.color = normalized;
    if (remember) this.recentColors = rememberColor(normalized);
    this.renderProperties();
  }

  private openInlineText(point: Point): void {
    if (this.inlineEditor) return;
    const tool = this.current;
    if (tool !== "text" && tool !== "tag") return;
    const shapeColor = this.color;
    const shapeWidth = this.width;
    const displayFontSize = tool === "tag" ? 20 : shapeWidth * 3 + 16;
    this.inlineEditor = new InlineTextEditor({
      canvas: this.canvas,
      point,
      color: shapeColor,
      displayFontSize,
      placeholder: tool === "tag" ? "输入标签" : "输入文字",
      singleLine: tool === "tag",
      onSubmit: (text) => {
        this.inlineEditor = null;
        this.shapes.push({
          kind: tool,
          x: point.x,
          y: point.y,
          x2: point.x,
          y2: point.y,
          color: shapeColor,
          width: shapeWidth,
          text,
        });
        this.redraw();
        this.notify(tool === "tag" ? "已添加标签标注" : "已添加文字标注");
      },
      onCancel: () => {
        this.inlineEditor = null;
      },
    });
  }

  private cancelInlineEditor(): void {
    if (!this.inlineEditor) return;
    const editor = this.inlineEditor;
    this.inlineEditor = null;
    editor.cancel();
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
    refreshPinstillIcons();
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
