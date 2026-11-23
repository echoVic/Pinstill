// Shared annotation engine: the drawing primitives of the selection overlay and a
// small reusable surface used by the in-place overlay annotation mode.
//
// The primitives are pure canvas work: callers own the canvas, its base image
// (a real capture drawn from a Blob-decoded ImageBitmap, so the canvas stays
// origin-clean for mosaic sampling and PNG export) and the toolbar UI.

import { annotationTextFont, arrowHead, hitsShape, shapeBounds, DEFAULT_TEXT_FONT, type ShapeBounds } from "./annotation-geometry";

export { DEFAULT_TEXT_FONT } from "./annotation-geometry";

export interface Point {
  x: number;
  y: number;
}

export interface Shape {
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
  /** Text shapes: font size in logical pt (independent of the width slider). */
  fontSize?: number;
  bold?: boolean;
  italic?: boolean;
  /** CSS font-family stack for text shapes. */
  fontFamily?: string;
  points?: Point[];
}

type ShapeStyleChange = Partial<Pick<Shape, "color" | "width" | "text" | "fontSize" | "fontFamily" | "bold" | "italic">>;

interface ShapeEdit {
  index: number;
  before: Shape | null;
  after: Shape | null;
}

const ANNOTATION_HISTORY_LIMIT = 200;

export interface ShapeRenderStyle {
  /** Canvas pixels per logical design pixel (line widths, fonts, mosaic cells). */
  scale: number;
  /** 10..90: watermark shapes render with this opacity. */
  watermarkOpacity: number;
  canvas: HTMLCanvasElement;
  /** True while the shape is the in-progress draft (cheap mosaic preview). */
  preview?: boolean;
}

/** A tiled watermark laid over the whole capture (not a movable shape). */
export interface WatermarkLayer {
  text: string;
  /** 10..90: the tiled text renders with this opacity. */
  opacity: number;
  /** Font size in logical pt (scaled by the canvas scale). */
  size: number;
  color: string;
}

/** Watermark marks are drawn diagonally, like the mainstream tools. */
const WATERMARK_ANGLE = (-30 * Math.PI) / 180;

/** Draws the sparse, diagonal tiled watermark across the whole canvas. */
export function drawTiledWatermark(
  ctx: CanvasRenderingContext2D,
  layer: WatermarkLayer,
  scale: number,
  canvas: HTMLCanvasElement,
): void {
  const text = layer.text.trim();
  if (!text) return;
  const fontSize = Math.max(10, layer.size) * scale;
  ctx.save();
  ctx.globalAlpha = Math.max(0.05, Math.min(1, layer.opacity / 100));
  ctx.fillStyle = layer.color;
  ctx.font = `600 ${fontSize}px ${DEFAULT_TEXT_FONT}`;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  const textWidth = Math.max(1, ctx.measureText(text).width);
  // Generous spacing: a watermark marks the image without covering it.
  const stepX = textWidth + fontSize * 6;
  const stepY = fontSize * 7;
  const cx = canvas.width / 2;
  const cy = canvas.height / 2;
  const extent = Math.hypot(canvas.width, canvas.height) / 2 + Math.max(textWidth, stepY);
  ctx.translate(cx, cy);
  ctx.rotate(WATERMARK_ANGLE);
  let row = 0;
  for (let y = -extent; y <= extent; y += stepY, row++) {
    const offset = row % 2 === 0 ? 0 : stepX / 2;
    for (let x = -extent + offset; x <= extent; x += stepX) {
      ctx.fillText(text, x, y);
    }
  }
  ctx.restore();
}

export function roundRectPath(
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

export function strokePath(ctx: CanvasRenderingContext2D, points: Point[]): void {
  ctx.beginPath();
  points.forEach((point, index) => {
    if (index === 0) ctx.moveTo(point.x, point.y);
    else ctx.lineTo(point.x, point.y);
  });
  ctx.stroke();
}

function drawMosaic(
  ctx: CanvasRenderingContext2D,
  shape: Shape,
  x: number,
  y: number,
  w: number,
  h: number,
  style: ShapeRenderStyle,
): void {
  const cell = Math.max(2, Math.round((shape.width * 2 + 4) * style.scale));
  const columns = Math.ceil(w / cell);
  const rows = Math.ceil(h / cell);
  if (style.preview && columns * rows > 24_000) {
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
  const right = Math.min(style.canvas.width, Math.ceil(x + w));
  const bottom = Math.min(style.canvas.height, Math.ceil(y + h));
  if (right <= left || bottom <= top) return;

  const sample = document.createElement("canvas");
  sample.width = right - left;
  sample.height = bottom - top;
  const sampleCtx = sample.getContext("2d", { willReadFrequently: true });
  if (!sampleCtx) return;
  sampleCtx.drawImage(
    style.canvas,
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

/** Draws one annotation shape on the canvas. */
export function drawShape(
  ctx: CanvasRenderingContext2D,
  shape: Shape,
  style: ShapeRenderStyle,
): void {
  const scale = style.scale;
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
    strokePath(ctx, [
      { x: shape.x, y: shape.y },
      { x: shape.x2, y: shape.y2 },
    ]);
    if (shape.kind === "arrow") strokePath(ctx, arrowHead(shape, scale));
  }
  if (shape.kind === "brush" && shape.points) strokePath(ctx, shape.points);
  if (shape.kind === "highlight") {
    ctx.globalAlpha = 0.32;
    ctx.fillRect(x, y, w, h);
    ctx.globalAlpha = 1;
  }
  if (shape.kind === "text") {
    const fontSize = (shape.fontSize ?? 14) * scale;
    ctx.font = annotationTextFont(shape, fontSize);
    const lines = (shape.text ?? "").split("\n");
    const lineHeight = fontSize * 1.3;
    lines.forEach((line, index) => {
      ctx.fillText(line, shape.x, shape.y + index * lineHeight);
    });
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
    ctx.globalAlpha = style.watermarkOpacity / 100;
    ctx.font = `bold ${(shape.width * 3 + 18) * scale}px -apple-system, sans-serif`;
    ctx.fillText(shape.text ?? "", shape.x, shape.y);
    ctx.globalAlpha = 1;
  }
  if (shape.kind === "mosaic") {
    ctx.translate(-(shape.dx ?? 0), -(shape.dy ?? 0));
    drawMosaic(ctx, shape, x + (shape.dx ?? 0), y + (shape.dy ?? 0), w, h, style);
  }
  ctx.restore();
}

export interface AnnotationStyle {
  color: string;
  /** 1..10: line width / font-size weight, as in the design's slider. */
  width: number;
  /** Text used by text / tag / watermark shapes. */
  entry: string;
}

export interface AnnotationEngineOptions {
  canvas: HTMLCanvasElement;
  getStyle: () => AnnotationStyle;
  getWatermarkOpacity: () => number;
  /** Called after every redraw (toolbar undo state, size label, …). */
  onDraw?: () => void;
}

/**
 * A reusable interactive annotation surface: draws shapes over a base image the
 * caller renders through `setBaseRenderer`. Text/tag/watermark shapes place on
 * click; brush/rect/ellipse/line/arrow/mosaic/highlight drag; `select` picks
 * an existing shape for editing.
 */
export class AnnotationEngine {
  readonly canvas: HTMLCanvasElement;

  private readonly ctx: CanvasRenderingContext2D;
  private readonly options: AnnotationEngineOptions;
  private shapes: Shape[] = [];
  private past: ShapeEdit[] = [];
  private future: ShapeEdit[] = [];
  private selectedIndex = -1;
  private draft: Shape | null = null;
  private start: Point | null = null;
  private movePreview: Shape | null = null;
  private tool = "rect";
  private scale = 1;
  private labelNumber = 1;
  private baseRenderer: (() => void) | null = null;
  private watermark: WatermarkLayer | null = null;

  constructor(options: AnnotationEngineOptions) {
    this.canvas = options.canvas;
    const ctx = this.canvas.getContext("2d");
    if (!ctx) throw new Error("2D canvas is not available");
    this.ctx = ctx;
    this.options = options;
  }

  setBaseRenderer(renderer: (() => void) | null): void {
    this.baseRenderer = renderer;
  }

  /** Sets (or clears) the tiled watermark drawn over the capture and under the
   * annotations; the watermark is part of every exported image. */
  setWatermark(layer: WatermarkLayer | null): void {
    this.watermark = layer ? { ...layer } : null;
    this.redraw();
  }

  getWatermark(): WatermarkLayer | null {
    return this.watermark ? { ...this.watermark } : null;
  }

  /** canvas pixels per logical design pixel (capture pixels / selection CSS px). */
  setScale(scale: number): void {
    this.scale = Number.isFinite(scale) && scale > 0 ? scale : 1;
  }

  getScale(): number {
    return this.scale;
  }

  setTool(tool: string): void {
    if (tool === this.tool) return;
    this.tool = tool;
    this.selectedIndex = -1;
    this.cancelDraw();
  }

  getTool(): string {
    return this.tool;
  }

  getShapes(): ReadonlyArray<Shape> {
    return this.shapes;
  }

  canUndo(): boolean {
    return this.past.length > 0;
  }

  canRedo(): boolean {
    return this.future.length > 0;
  }

  getSelectedShape(): Readonly<Shape> | null {
    return this.shapes[this.selectedIndex] ?? null;
  }

  getSelectionBounds(): ShapeBounds | null {
    const shape = this.movePreview ?? this.getSelectedShape();
    if (!shape) return null;
    const bounds = shapeBounds(this.ctx, shape, this.scale);
    const margin = (3 + shape.width / 2) * this.scale;
    return {
      x: bounds.x + (shape.dx ?? 0) - margin,
      y: bounds.y + (shape.dy ?? 0) - margin,
      width: bounds.width + 2 * margin,
      height: bounds.height + 2 * margin,
    };
  }

  editSelectedShape(update: ShapeStyleChange): boolean {
    const shape = this.shapes[this.selectedIndex];
    if (!shape || this.start) return false;
    const keys = Object.keys(update) as (keyof ShapeStyleChange)[];
    if (!keys.some((key) => update[key] !== shape[key])) return false;
    this.commit({ index: this.selectedIndex, before: shape, after: { ...shape, ...update } });
    return true;
  }

  deleteSelectedShape(): boolean {
    const shape = this.shapes[this.selectedIndex];
    if (!shape || this.start) return false;
    this.commit({ index: this.selectedIndex, before: shape, after: null });
    return true;
  }

  reset(): void {
    this.shapes = [];
    this.past = [];
    this.future = [];
    this.selectedIndex = -1;
    this.draft = null;
    this.start = null;
    this.movePreview = null;
    this.labelNumber = 1;
  }

  /** Adds a shape created outside the pointer flow (e.g. inline text input). */
  addShape(shape: Shape): void {
    this.commit({ index: this.shapes.length, before: null, after: shape });
  }

  undo(): boolean {
    if (this.start) return false;
    const edit = this.past.pop();
    if (!edit) return false;
    this.future.push(edit);
    this.applyEdit(edit.index, edit.after, edit.before);
    return true;
  }

  redo(): boolean {
    if (this.start) return false;
    const edit = this.future.pop();
    if (!edit) return false;
    this.past.push(edit);
    this.applyEdit(edit.index, edit.before, edit.after);
    return true;
  }

  private applyEdit(index: number, oldShape: Shape | null, newShape: Shape | null): void {
    this.shapes.splice(index, oldShape ? 1 : 0, ...(newShape ? [newShape] : []));
    this.selectedIndex = this.tool === "select" && newShape ? index : -1;
    this.redraw();
  }

  private commit(edit: ShapeEdit): void {
    this.past.push(edit);
    if (this.past.length > ANNOTATION_HISTORY_LIMIT) this.past.shift();
    this.future = [];
    this.applyEdit(edit.index, edit.before, edit.after);
  }

  redraw(): void {
    const { width, height } = this.canvas;
    this.ctx.clearRect(0, 0, width, height);
    this.baseRenderer?.();
    if (this.watermark) {
      drawTiledWatermark(this.ctx, this.watermark, this.scale, this.canvas);
    }
    this.shapes.forEach((shape, index) => {
      const preview = index === this.selectedIndex && this.movePreview;
      drawShape(this.ctx, preview || shape, {
        scale: this.scale,
        watermarkOpacity: this.options.getWatermarkOpacity(),
        canvas: this.canvas,
        preview: Boolean(preview),
      });
    });
    if (this.draft) {
      drawShape(this.ctx, this.draft, {
        scale: this.scale,
        watermarkOpacity: this.options.getWatermarkOpacity(),
        canvas: this.canvas,
        preview: true,
      });
    }
    this.options.onDraw?.();
  }

  /** Starts a drag/draw at a canvas-space point. Returns whether it did. */
  beginDraw(point: Point): boolean {
    if (this.tool === "select") {
      this.selectedIndex = -1;
      for (let index = this.shapes.length - 1; index >= 0; index--) {
        if (!hitsShape(this.ctx, this.shapes[index]!, this.scale, point)) continue;
        this.selectedIndex = index;
        break;
      }
      this.start = this.selectedIndex >= 0 ? point : null;
      this.redraw();
      return this.start !== null;
    }
    if (!this.isDrawingTool(this.tool)) return false;
    const style = this.options.getStyle();
    this.start = point;
    this.draft = {
      kind: this.tool,
      x: point.x,
      y: point.y,
      x2: point.x,
      y2: point.y,
      color: style.color,
      width: style.width,
      points: [point],
      text: this.tool === "tag" ? String(this.labelNumber++) : style.entry,
    };
    if (this.tool === "text" || this.tool === "tag") {
      const placed = this.draft;
      this.draft = null;
      this.start = null;
      this.addShape(placed);
    }
    return true;
  }

  moveDraw(point: Point): void {
    const selected = this.shapes[this.selectedIndex];
    if (selected && this.start) {
      const dx = point.x - this.start.x;
      const dy = point.y - this.start.y;
      this.movePreview = Math.hypot(dx, dy) < this.scale ? null : {
        ...selected,
        dx: (selected.dx ?? 0) + dx,
        dy: (selected.dy ?? 0) + dy,
      };
      this.redraw();
      return;
    }
    if (!this.draft) return;
    this.draft.x2 = point.x;
    this.draft.y2 = point.y;
    if (this.draft.kind === "brush" && this.draft.points) this.draft.points.push(point);
    this.redraw();
  }

  endDraw(): void {
    const selected = this.shapes[this.selectedIndex];
    const moved = this.movePreview;
    const draft = this.draft;
    this.movePreview = null;
    this.draft = null;
    this.start = null;
    if (selected && moved) {
      this.commit({ index: this.selectedIndex, before: selected, after: moved });
      return;
    }
    if (draft) {
      const points = draft.kind === "brush" ? draft.points ?? [] : [{ x: draft.x2, y: draft.y2 }];
      if (points.some((point) => Math.abs(point.x - draft.x) + Math.abs(point.y - draft.y) > 3)) {
        this.addShape(draft);
        return;
      }
    }
    this.redraw();
  }

  cancelDraw(): void {
    this.draft = null;
    this.start = null;
    this.movePreview = null;
    this.redraw();
  }

  isDrawing(): boolean {
    return this.start !== null;
  }

  /** The composited PNG (base image + shapes) as raw base64, no `data:` prefix. */
  toPNGBase64(): string {
    const dataURL = this.canvas.toDataURL("image/png");
    const comma = dataURL.indexOf(",");
    return comma >= 0 ? dataURL.slice(comma + 1) : dataURL;
  }

  private isDrawingTool(tool: string): boolean {
    return (
      tool === "rect" ||
      tool === "ellipse" ||
      tool === "line" ||
      tool === "arrow" ||
      tool === "brush" ||
      tool === "text" ||
      tool === "tag" ||
      tool === "mosaic" ||
      tool === "highlight"
    );
  }
}

export interface InlineTextOptions {
  canvas: HTMLCanvasElement;
  /** Where the text is placed, in canvas pixels. */
  point: Point;
  color: string;
  /** Font size in CSS pixels (what the user sees while typing). */
  displayFontSize: number;
  bold?: boolean;
  italic?: boolean;
  /** CSS font-family stack for the editing textarea. */
  fontFamily?: string;
  value?: string;
  placeholder?: string;
  /** Labels (tags) are single-line: newlines become spaces. */
  singleLine?: boolean;
  onSubmit: (text: string) => void;
  onCancel?: () => void;
}

/**
 * The in-place text input of the annotation tools: a textarea opened exactly
 * where the user clicked, positioned over the canvas (display coordinates),
 * with the same font size the committed annotation will have.
 *
 * Enter commits, Shift+Enter inserts a newline, Esc cancels, blur commits;
 * empty text is discarded; Enter during IME composition never commits.
 */
export class InlineTextEditor {
  readonly element: HTMLTextAreaElement;

  private readonly options: InlineTextOptions;
  private done = false;
  private composing = false;

  constructor(options: InlineTextOptions) {
    this.options = options;
    const textarea = document.createElement("textarea");
    textarea.id = "textInputOverlay";
    textarea.className = "inline-text-input";
    textarea.value = options.value ?? "";
    textarea.placeholder = options.placeholder ?? "输入文字";
    textarea.rows = 1;
    textarea.spellcheck = false;
    textarea.setAttribute("aria-label", options.placeholder ?? "输入文字");

    const rect = options.canvas.getBoundingClientRect();
    const scaleX = rect.width > 0 ? rect.width / options.canvas.width : 1;
    const scaleY = rect.height > 0 ? rect.height / options.canvas.height : 1;
    const fontSize = Math.max(11, options.displayFontSize);
    const width = Math.max(160, Math.min(320, rect.width * 0.4));
    let left = rect.left + options.point.x * scaleX;
    let top = rect.top + options.point.y * scaleY - fontSize * 0.85;
    left = Math.min(Math.max(4, left), Math.max(4, window.innerWidth - width - 8));
    top = Math.min(Math.max(4, top), Math.max(4, window.innerHeight - fontSize * 3));

    Object.assign(textarea.style, {
      position: "fixed",
      left: `${Math.round(left)}px`,
      top: `${Math.round(top)}px`,
      width: `${Math.round(width)}px`,
      minHeight: `${Math.round(fontSize * 1.5)}px`,
      maxHeight: "220px",
      padding: "2px 5px",
      margin: "0",
      border: "1px dashed rgba(243, 162, 78, 0.95)",
      borderRadius: "4px",
      background: "rgba(20, 20, 18, 0.88)",
      color: options.color,
      font: `${options.italic ? "italic " : ""}${options.bold ? "700" : "400"} ${fontSize}px ${options.fontFamily ?? DEFAULT_TEXT_FONT}`,
      lineHeight: "1.3",
      outline: "none",
      resize: "none",
      overflow: "hidden",
      zIndex: "30",
      boxShadow: "0 8px 24px rgba(0, 0, 0, 0.45)",
      caretColor: options.color,
    });

    const autosize = () => {
      textarea.style.height = "auto";
      textarea.style.height = `${Math.min(textarea.scrollHeight, 220)}px`;
    };
    textarea.addEventListener("input", autosize);
    textarea.addEventListener("compositionstart", () => {
      this.composing = true;
    });
    textarea.addEventListener("compositionend", () => {
      this.composing = false;
    });
    textarea.addEventListener("keydown", (event) => {
      if (event.isComposing || this.composing || event.keyCode === 229) return;
      event.stopPropagation();
      if (event.key === "Escape") {
        event.preventDefault();
        this.cancel();
        return;
      }
      if (event.key === "Enter" && !event.shiftKey) {
        event.preventDefault();
        this.submit();
      }
    });
    textarea.addEventListener("blur", () => {
      if (!this.done) this.submit();
    });

    document.body.append(textarea);
    this.element = textarea;
    window.requestAnimationFrame(() => {
      textarea.focus();
      const end = textarea.value.length;
      textarea.setSelectionRange(end, end);
      autosize();
    });
  }

  submit(): void {
    if (this.done) return;
    this.done = true;
    let text = this.element.value;
    if (this.options.singleLine) text = text.replace(/\s*\n+\s*/g, " ").replace(/\s+/g, " ");
    else text = text.replace(/\s+$/, "");
    const trimmed = text.trim();
    this.dispose();
    if (!trimmed) {
      this.options.onCancel?.();
      return;
    }
    this.options.onSubmit(text);
  }

  cancel(): void {
    if (this.done) return;
    this.done = true;
    this.dispose();
    this.options.onCancel?.();
  }

  private dispose(): void {
    this.element.remove();
  }
}
